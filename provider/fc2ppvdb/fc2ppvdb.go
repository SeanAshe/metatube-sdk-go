package fc2ppvdb

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/fc2/fc2util"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*FC2PPVDB)(nil)
	_ provider.ConfigSetter  = (*FC2PPVDB)(nil)
)

const (
	Name     = "FC2PPVDB"
	Priority = 1000 - 2
)

const (
	baseURL  = "https://fc2ppv-db.com/"
	movieURL = "https://fc2ppv-db.com/ja/videos/%s"
)

const (
	noInformation = "情報がありません"
	ageGatePath   = "/age-verify"
)

var (
	// fc2ppv-db.com sits behind Cloudflare. A browser clearance cookie is
	// bound to the user agent that solved it:
	// MT_MOVIE_PROVIDER_FC2PPVDB__COOKIE
	// MT_MOVIE_PROVIDER_FC2PPVDB__USER_AGENT
	errChallenge = fmt.Errorf("FC2PPV-DB: Cloudflare challenge, set MT_MOVIE_PROVIDER_FC2PPVDB__COOKIE")
	errAgeGate   = fmt.Errorf("FC2PPV-DB: age verification was not passed")

	metaTail = regexp.MustCompile(`販売者:\s*(.*?)\s*/\s*公開日:\s*([^/]*?)\s*/\s*再生時間:\s*([\d:]+)\s*$`)
)

// FC2PPVDB reads fc2ppv-db.com. The previous fc2ppvdb.com host is closed.
// The page is server-rendered: h1 is "FC2-PPV-<id> <title>", and 動画詳細情報
// carries the sale date, cast, seller, and tags. Runtime is only present at
// the end of the description meta tag.
type FC2PPVDB struct {
	*scraper.Scraper
	cookies   []*http.Cookie
	userAgent string
}

func New() *FC2PPVDB {
	return &FC2PPVDB{Scraper: scraper.NewDefaultScraper(
		Name, baseURL, Priority, language.Japanese,
		scraper.WithHeaders(map[string]string{
			"Referer":         baseURL,
			"Accept-Language": "ja,en;q=0.8",
		}),
		scraper.WithLimit(&colly.LimitRule{
			DomainGlob:  "*fc2ppv-db.com*",
			Parallelism: 1,
			Delay:       2 * time.Second,
		}),
	)}
}

func (fc2ppvdb *FC2PPVDB) SetConfig(config provider.Config) error {
	if config.Has("cookie") {
		if raw, err := config.GetString("cookie"); err == nil {
			fc2ppvdb.cookies = parseCookieHeader(raw)
		}
	}
	if config.Has("user_agent") {
		if raw, err := config.GetString("user_agent"); err == nil {
			fc2ppvdb.userAgent = strings.TrimSpace(raw)
		}
	}
	return nil
}

func (fc2ppvdb *FC2PPVDB) NormalizeMovieID(id string) string {
	return fc2util.ParseNumber(id)
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByID(id string) (*model.MovieInfo, error) {
	if number := fc2util.ParseNumber(id); number != "" {
		id = number
	}
	if id == "" {
		return nil, provider.ErrInvalidID
	}
	return fc2ppvdb.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (fc2ppvdb *FC2PPVDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !strings.Contains(homepage.Path, "/videos/") {
		return "", provider.ErrInvalidURL
	}
	id := fc2util.ParseNumber(pathBase(homepage.Path))
	if id == "" {
		return "", provider.ErrInvalidURL
	}
	return id, nil
}

func (fc2ppvdb *FC2PPVDB) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := fc2ppvdb.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}
	homepage := fmt.Sprintf(movieURL, id)

	c := fc2ppvdb.collector()
	c.OnError(func(r *colly.Response, e error) {
		if r != nil && (r.StatusCode == http.StatusForbidden || isChallenge(r.Body)) {
			err = errChallenge
			return
		}
		if r != nil && r.StatusCode == http.StatusNotFound {
			err = provider.ErrInfoNotFound
			return
		}
		err = e
	})
	c.OnResponse(func(r *colly.Response) {
		if isChallenge(r.Body) {
			err = errChallenge
			return
		}
		if r.Request != nil && r.Request.URL != nil && strings.Contains(r.Request.URL.Path, ageGatePath) {
			err = errAgeGate
			return
		}
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(r.Body)))
		if parseErr != nil {
			err = parseErr
			return
		}
		parsed, parseErr := parsePage(doc, id)
		if parseErr != nil {
			err = parseErr
			return
		}
		parsed.Homepage = homepage
		info = parsed
	})

	if vErr := c.Visit(homepage); vErr != nil && err == nil {
		err = vErr
	}
	if err == nil && info == nil {
		err = provider.ErrInfoNotFound
	}
	return
}

func (fc2ppvdb *FC2PPVDB) collector() *colly.Collector {
	c := fc2ppvdb.ClonedCollector()
	if fc2ppvdb.userAgent != "" {
		c.UserAgent = fc2ppvdb.userAgent
	}
	if len(fc2ppvdb.cookies) > 0 {
		_ = c.SetCookies(baseURL, fc2ppvdb.cookies)
	}
	return c
}

func parsePage(doc *goquery.Document, id string) (*model.MovieInfo, error) {
	heading := strings.TrimSpace(doc.Find("main h1").First().Text())
	if heading == "" {
		heading = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if !titleHasID(heading, id) {
		return nil, provider.ErrInfoNotFound
	}

	info := &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("FC2-%s", id),
		Provider:      Name,
		Title:         strings.TrimSpace(strings.TrimPrefix(heading, "FC2-PPV-"+id)),
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	description, _ := doc.Find(`meta[name="description"]`).Attr("content")
	tail := metaTail.FindStringSubmatch(description)
	if sold := labelled(doc, "販売日"); sold != "" {
		info.ReleaseDate = parser.ParseDate(sold)
	} else if len(tail) == 4 {
		info.ReleaseDate = parser.ParseDate(tail[2])
	}
	if len(tail) == 4 {
		info.Runtime = parser.ParseRuntime(tail[3])
	}

	info.Actors = performers(doc)
	if seller, _ := seller(doc); seller != "" {
		info.Maker = seller
	} else if len(tail) == 4 {
		info.Maker = strings.TrimSpace(tail[1])
	}
	info.Genres = tags(doc)
	if cover, ok := doc.Find(`meta[property="og:image"]`).Attr("content"); ok {
		info.CoverURL = strings.TrimSpace(cover)
		info.ThumbURL = info.CoverURL
	}
	return info, nil
}

func titleHasID(title, id string) bool {
	needle := "FC2-PPV-" + id
	at := strings.Index(title, needle)
	if at < 0 {
		return false
	}
	end := at + len(needle)
	return end >= len(title) || title[end] < '0' || title[end] > '9'
}

func labelled(doc *goquery.Document, label string) string {
	found := ""
	doc.Find("p").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if strings.TrimSpace(node.Text()) != label {
			return true
		}
		if next := node.NextAllFiltered("p").First(); next.Length() > 0 {
			found = strings.TrimSpace(next.Text())
		}
		return false
	})
	return found
}

func blockAfter(doc *goquery.Document, label string) *goquery.Selection {
	var block *goquery.Selection
	doc.Find("p, span").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if strings.TrimSpace(node.Text()) != label {
			return true
		}
		parent := node.Parent()
		if parent.Length() == 0 || parent.Parent().Length() == 0 {
			return true
		}
		block = parent.Parent()
		return false
	})
	return block
}

func performers(doc *goquery.Document) []string {
	block := blockAfter(doc, "出演女優")
	if block == nil {
		return []string{}
	}
	names := make([]string, 0)
	seen := map[string]struct{}{}
	block.Find(`a[href*="/actresses/"]`).Each(func(_ int, link *goquery.Selection) {
		name := strings.TrimSpace(link.Find("img[alt]").First().AttrOr("alt", ""))
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		if name == "" || name == noInformation {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	})
	return names
}

func seller(doc *goquery.Document) (string, string) {
	link := doc.Find(`main a[href*="/sellers/"]`).First()
	if link.Length() == 0 {
		link = doc.Find(`a[href*="/sellers/"]`).First()
	}
	if link.Length() == 0 {
		return "", ""
	}
	href := link.AttrOr("href", "")
	slug := strings.Trim(href, "/")
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	name := strings.TrimSpace(link.Find("img[alt]").First().AttrOr("alt", ""))
	if name == "" {
		name = strings.TrimSpace(strings.Replace(link.Text(), "販売者", "", 1))
	}
	return name, slug
}

func tags(doc *goquery.Document) []string {
	block := blockAfter(doc, "タグ")
	if block == nil {
		return []string{}
	}
	found := make([]string, 0)
	block.Find("a").Each(func(_ int, link *goquery.Selection) {
		if text := strings.TrimSpace(link.Text()); text != "" {
			found = append(found, text)
		}
	})
	return found
}

func isChallenge(body []byte) bool {
	text := string(body)
	return strings.Contains(text, "Just a moment") ||
		strings.Contains(text, "cf-browser-verification") ||
		strings.Contains(text, "/cdn-cgi/challenge-platform")
}

func parseCookieHeader(raw string) []*http.Cookie {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	header := http.Header{}
	header.Add("Cookie", raw)
	return (&http.Request{Header: header}).Cookies()
}

func pathBase(path string) string {
	path = strings.Trim(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func init() {
	provider.Register(Name, New)
}
