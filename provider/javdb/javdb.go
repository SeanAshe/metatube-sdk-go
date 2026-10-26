package javdb

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
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*JavDB)(nil)
	_ provider.MovieSearcher = (*JavDB)(nil)
)

const (
	Name     = "JAVDB"
	Priority = 1000 - 8
)

const (
	baseURL   = "https://javdb.com/"
	movieURL  = "https://javdb.com/v/%s"
	searchURL = "https://javdb.com/search?q=%s&f=all&locale=zh"
)

var (
	errLoginRequired = fmt.Errorf("javdb: login required")

	slugPattern = regexp.MustCompile(`^[A-Za-z0-9]{4,8}$`)
	codePattern = regexp.MustCompile(`(?i)^[a-z]{2,}\d{2,}$`)
	codeDash    = regexp.MustCompile(`^([A-Z]{2,})(\d+)$`)
	loginTitle  = regexp.MustCompile(`(?s)<title>\s*登入\s*\|`)
	scoreValue  = regexp.MustCompile(`(\d+(?:\.\d+)?)`)
)

// JavDB reads javdb.com. Detail pages are requested with locale=zh so the
// panel labels stay 番號/日期/片商/發行/系列/導演/演員/時長/類別. The host
// quota is shared with image hosts; one request every 3 seconds matches the
// rate that stays under the ban threshold.
type JavDB struct {
	*scraper.Scraper
}

func New() *JavDB {
	return &JavDB{scraper.NewDefaultScraper(
		Name, baseURL, Priority, language.Japanese,
		scraper.WithHeaders(map[string]string{
			"Referer": baseURL,
		}),
		scraper.WithCookies(baseURL, []*http.Cookie{
			{Name: "over18", Value: "1"},
			{Name: "locale", Value: "zh"},
		}),
		scraper.WithRequestTimeout(30*time.Second),
		scraper.WithLimit(&colly.LimitRule{
			DomainGlob:  "*javdb.com*",
			Parallelism: 1,
			Delay:       3 * time.Second,
		}),
	)}
}

func (jav *JavDB) NormalizeMovieID(id string) string {
	id = strings.TrimSpace(id)
	if isSlug(id) {
		return id
	}
	return strings.ToUpper(id)
}

func (jav *JavDB) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	if isSlug(id) {
		info, err = jav.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
		if err == nil || err != provider.ErrInfoNotFound {
			return
		}
	}
	return jav.getByCode(id)
}

func (jav *JavDB) getByCode(id string) (*model.MovieInfo, error) {
	keyword := jav.NormalizeMovieKeyword(id)
	if keyword == "" {
		return nil, provider.ErrInvalidID
	}
	results, err := jav.SearchMovie(keyword)
	if err != nil {
		return nil, err
	}
	want := compactCode(keyword)
	for _, result := range results {
		if result != nil && compactCode(result.Number) == want {
			return jav.GetMovieInfoByURL(result.Homepage)
		}
	}
	return nil, provider.ErrInfoNotFound
}

func (jav *JavDB) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if !strings.Contains(homepage.Hostname(), "javdb.com") {
		return "", provider.ErrInvalidURL
	}
	parts := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "v" && parts[1] != "" {
		return parts[1], nil
	}
	return "", provider.ErrInvalidURL
}

func (jav *JavDB) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := jav.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	homepage, err := url.Parse(fmt.Sprintf(movieURL, id))
	if err != nil {
		return
	}
	info = &model.MovieInfo{
		ID:            id,
		Provider:      jav.Name(),
		Homepage:      homepage.String(),
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := jav.ClonedCollector()
	c.OnError(func(r *colly.Response, e error) {
		if r != nil && r.StatusCode == http.StatusNotFound {
			err = provider.ErrInfoNotFound
			return
		}
		err = e
	})
	c.OnResponse(func(r *colly.Response) {
		if loginTitle.Match(r.Body) {
			err = errLoginRequired
			return
		}
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(r.Body)))
		if parseErr != nil {
			err = parseErr
			return
		}
		if applyErr := applyDetail(doc, info); applyErr != nil {
			err = applyErr
		}
	})

	visitURL := *homepage
	query := visitURL.Query()
	query.Set("locale", "zh")
	visitURL.RawQuery = query.Encode()
	if vErr := c.Visit(visitURL.String()); vErr != nil && err == nil {
		err = vErr
	}
	return
}

func (jav *JavDB) NormalizeMovieKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return ""
	}
	if isSlug(keyword) {
		return keyword
	}
	keyword = strings.ToUpper(keyword)
	return codeDash.ReplaceAllString(keyword, "${1}-${2}")
}

func (jav *JavDB) SearchMovie(keyword string) (results []*model.MovieSearchResult, err error) {
	c := jav.ClonedCollector()
	c.OnError(func(r *colly.Response, e error) {
		err = e
	})
	c.OnResponse(func(r *colly.Response) {
		if loginTitle.Match(r.Body) {
			err = errLoginRequired
			return
		}
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(r.Body)))
		if parseErr != nil {
			err = parseErr
			return
		}
		results = parseSearch(doc, jav.Name())
	})

	if vErr := c.Visit(fmt.Sprintf(searchURL, url.QueryEscape(keyword))); vErr != nil && err == nil {
		err = vErr
	}
	return
}

func applyDetail(doc *goquery.Document, info *model.MovieInfo) error {
	if title := strings.TrimSpace(doc.Find("span.origin-title").First().Text()); title != "" {
		info.Title = title
	} else {
		info.Title = strings.TrimSpace(doc.Find("strong.current-title").First().Text())
	}
	if src, ok := doc.Find("img.video-cover").First().Attr("src"); ok {
		info.CoverURL = strings.TrimSpace(src)
		info.ThumbURL = info.CoverURL
	}

	doc.Find("div.panel-block").Each(func(_ int, block *goquery.Selection) {
		label := strings.Trim(strings.TrimSpace(block.Find("strong").First().Text()), ":：")
		value := block.Find("span.value").First()
		switch label {
		case "番號", "品番", "ID":
			info.Number = compactNumber(value.Text())
		case "日期", "発売日", "配信開始日", "Released Date":
			info.ReleaseDate = parser.ParseDate(strings.TrimSpace(value.Text()))
		case "時長", "収録時間", "Duration":
			info.Runtime = parser.ParseRuntime(value.Text())
		case "導演", "監督", "Director":
			info.Director = strings.TrimSpace(value.Text())
		case "片商", "メーカー", "Maker":
			info.Maker = preferJapanese(strings.TrimSpace(value.Text()))
		case "發行", "レーベル", "Publisher":
			info.Label = strings.TrimSpace(value.Text())
		case "系列", "シリーズ", "Series":
			info.Series = strings.TrimSpace(value.Text())
		case "評分", "評価", "Rating":
			if ss := scoreValue.FindStringSubmatch(value.Text()); len(ss) == 2 {
				info.Score = parser.ParseScore(ss[1])
			}
		case "類別", "ジャンル", "Tags":
			info.Genres = appendUnique(info.Genres[:0], linkTexts(value)...)
		case "演員", "出演者", "Actor(s)", "Actors":
			value.Find("a.actor-female").Each(func(_ int, link *goquery.Selection) {
				if name := strings.TrimSpace(link.Text()); name != "" {
					info.Actors = appendUnique(info.Actors, name)
				}
			})
		}
	})

	doc.Find("a.tile-item").Each(func(_ int, link *goquery.Selection) {
		href, ok := link.Attr("href")
		if !ok || href == "" || strings.Contains(href, "/login") {
			return
		}
		if abs := resolveURL(info.Homepage, href); abs != "" {
			info.PreviewImages = appendUnique(info.PreviewImages, abs)
		}
	})

	if info.Title == "" || info.Number == "" {
		return provider.ErrInfoNotFound
	}
	return nil
}

func parseSearch(doc *goquery.Document, providerName string) []*model.MovieSearchResult {
	var results []*model.MovieSearchResult
	seen := map[string]struct{}{}
	doc.Find("div.movie-list a.box").Each(func(_ int, box *goquery.Selection) {
		href, ok := box.Attr("href")
		if !ok {
			return
		}
		homepage := resolveURL(baseURL, href)
		id := pathID(homepage)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}

		number := strings.TrimSpace(box.Find("strong").First().Text())
		title := strings.TrimSpace(box.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(strings.TrimPrefix(box.Find("div.video-title").Text(), number))
		}
		thumb, _ := box.Find("img").First().Attr("src")
		score := 0.0
		if ss := scoreValue.FindStringSubmatch(box.Find("span.value").First().Text()); len(ss) == 2 {
			score = parser.ParseScore(ss[1])
		}
		results = append(results, &model.MovieSearchResult{
			ID:          id,
			Number:      number,
			Title:       title,
			Provider:    providerName,
			Homepage:    homepage,
			ThumbURL:    strings.TrimSpace(thumb),
			CoverURL:    strings.TrimSpace(thumb),
			Score:       score,
			ReleaseDate: parser.ParseDate(strings.TrimSpace(box.Find("div.meta").First().Text())),
		})
	})
	return results
}

func isSlug(id string) bool {
	return slugPattern.MatchString(id) && !codePattern.MatchString(id)
}

func compactCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r == '-' || r == '_' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func compactNumber(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\t', '\r', '\u00a0':
			return -1
		default:
			return r
		}
	}, s)
}

func preferJapanese(value string) string {
	parts := splitList(value)
	for _, part := range parts {
		if containsJapanese(part) {
			return part
		}
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func containsJapanese(s string) bool {
	for _, r := range s {
		if (r >= 0x3040 && r <= 0x30FF) || (r >= 0x4E00 && r <= 0x9FFF) {
			return true
		}
	}
	return false
}

func linkTexts(s *goquery.Selection) []string {
	var texts []string
	s.Find("a").Each(func(_ int, link *goquery.Selection) {
		if text := strings.TrimSpace(link.Text()); text != "" {
			texts = append(texts, text)
		}
	})
	return texts
}

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, item := range dst {
		seen[item] = struct{}{}
	}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func pathID(rawURL string) string {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "v" {
		return parts[1]
	}
	return ""
}

func resolveURL(base, href string) string {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil || ref.String() == "" {
		return ""
	}
	if ref.IsAbs() {
		ref.RawQuery = ""
		ref.Fragment = ""
		return ref.String()
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	abs := baseURL.ResolveReference(ref)
	abs.RawQuery = ""
	abs.Fragment = ""
	return abs.String()
}

func init() {
	provider.Register(Name, New)
}
