package fc2ppvdb

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"

	"github.com/metatube-community/metatube-sdk-go/internal/envconfig"
	"github.com/metatube-community/metatube-sdk-go/provider"
)

const dbCover = "https://d39jz7pbpqkw9s.cloudfront.net/thumbnails/48/4898837.webp"

func dbActress(at int) string {
	return fmt.Sprintf("5fb206c2-f8a0-4447-8e6e-%012d", at)
}

func dbPage(video, title, seller, slug, sold, runtime string, performers []string, leaked string, tags []string) string {
	var cast strings.Builder
	if len(performers) == 0 {
		cast.WriteString("<p>情報がありません</p>")
	}
	for at, name := range performers {
		cast.WriteString(`<a href="/ja/actresses/` + dbActress(at) + `"><span><img alt="` + name + `" src="/a.webp"></span><span>` + name + `</span></a>`)
	}
	var marks strings.Builder
	for _, tag := range tags {
		marks.WriteString(`<a href="/ja/videos?tags=` + tag + `">` + tag + `</a>`)
	}
	return `<html><head><meta name="description" content="FC2-PPV-` + video + ` ` + title +
		` - 販売者: ` + seller + ` / 公開日: 2026年5月9日 / 再生時間: ` + runtime + `">` +
		`<meta property="og:image" content="` + dbCover + `"></head>` +
		`<body><nav><a href="/ja/actresses/pr-uuid">今日の女優</a></nav><main>` +
		`<h1>FC2-PPV-` + video + ` ` + title + `</h1>` +
		`<section><h2>動画詳細情報</h2>` +
		`<div><div><p>動画ID</p></div><div><p>` + video + `</p></div></div>` +
		"<div>\n<p>販売日</p>\n<p>" + sold + "</p>\n</div>" +
		`<div><span>` + leaked + `</span><span>モザイクあり</span></div>` +
		`<div><div><p>出演女優</p></div><div>` + cast.String() + `</div></div>` +
		`<div><a href="/ja/sellers/` + slug + `"><span><img alt="` + seller + `" src="/s.webp"></span><span><span>販売者</span><span>` + seller + `</span></span></a></div>` +
		`<div><div><span>タグ</span></div><div>` + marks.String() + `</div></div>` +
		`</section></main>` +
		`<aside><h2>関連動画</h2><a href="/ja/actresses/other-uuid"><span>別の人</span></a></aside>` +
		`</body></html>`
}

func TestFC2PPVDB_ParsePage(t *testing.T) {
	page := dbPage("4898837", "ふたりの巨乳美少女と3P。", "ぷにぷに製作所", "punipuni",
		"2026年5月10日", "55:00", []string{"川北すずね"}, "流出なし", []string{"巨乳", "3P"})
	info, err := parsePage(mustDoc(t, page), "4898837")
	require.NoError(t, err)
	require.Equal(t, "4898837", info.ID)
	require.Equal(t, "FC2-4898837", info.Number)
	require.Equal(t, "ふたりの巨乳美少女と3P。", info.Title)
	require.Equal(t, []string{"川北すずね"}, []string(info.Actors))
	require.Equal(t, "ぷにぷに製作所", info.Maker)
	require.Equal(t, []string{"巨乳", "3P"}, []string(info.Genres))
	require.Equal(t, 55, info.Runtime)
	require.Equal(t, "2026-05-10", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, dbCover, info.CoverURL)
	info.Homepage = "https://fc2ppv-db.com/ja/videos/4898837"
	require.True(t, info.IsValid())

	two, err := parsePage(mustDoc(t, dbPage("4898837", "タイトル", "店", "slug",
		"2026年5月10日", "55:00", []string{"A", "B", "A"}, "流出あり", []string{"巨乳"})), "4898837")
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, []string(two.Actors))

	none, err := parsePage(mustDoc(t, dbPage("4898837", "タイトル", "店", "slug",
		"2026年5月10日", "55:00", nil, "流出なし", nil)), "4898837")
	require.NoError(t, err)
	require.Empty(t, none.Actors)

	missingSale := strings.ReplaceAll(page, "<div>\n<p>販売日</p>\n<p>2026年5月10日</p>\n</div>", "")
	fallback, err := parsePage(mustDoc(t, missingSale), "4898837")
	require.NoError(t, err)
	require.Equal(t, "2026-05-09", time.Time(fallback.ReleaseDate).Format("2006-01-02"))
}

func TestFC2PPVDB_ParsePageRejects(t *testing.T) {
	missing := `<html><head><title>404</title></head><body><main><h1>404</h1><p>ページが見つかりません</p></main></body></html>`
	_, err := parsePage(mustDoc(t, missing), "4898837")
	require.ErrorIs(t, err, provider.ErrInfoNotFound)

	longer := dbPage("48988370", "タイトル", "店", "slug", "2026年5月10日", "55:00", nil, "流出なし", nil)
	_, err = parsePage(mustDoc(t, longer), "4898837")
	require.ErrorIs(t, err, provider.ErrInfoNotFound)

	require.True(t, isChallenge([]byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head></html>`)))
	id, parseErr := New().ParseMovieIDFromURL("https://fc2ppv-db.com/ja/videos/4898837")
	require.NoError(t, parseErr)
	require.Equal(t, "4898837", id)
	_, parseErr = New().ParseMovieIDFromURL("https://fc2ppv-db.com/ja/age-verify")
	require.ErrorIs(t, parseErr, provider.ErrInvalidURL)
}

func TestFC2PPVDB_SetConfig(t *testing.T) {
	cfg := envconfig.NewConfig()
	cfg.Set("cookie", "cf_clearance=abc; session=xyz")
	cfg.Set("user_agent", "Browser/1.0")
	db := New()
	require.NoError(t, db.SetConfig(cfg))
	require.Equal(t, "Browser/1.0", db.userAgent)
	require.Len(t, db.cookies, 2)
	require.Equal(t, "cf_clearance", db.cookies[0].Name)
	require.Equal(t, "abc", db.cookies[0].Value)
}

func TestFC2PPVDB_GetMovieInfoByID(t *testing.T) {
	if ci, _ := strconv.ParseBool(os.Getenv("GITHUB_ACTIONS")); ci {
		t.SkipNow()
	}
	info, err := New().GetMovieInfoByID("4898837")
	if errors.Is(err, errChallenge) || errors.Is(err, errAgeGate) {
		t.Skipf("fc2ppv-db.com is gated: %v", err)
	}
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "4898837", info.ID)
	require.Equal(t, "FC2-4898837", info.Number)
	require.NotEmpty(t, info.Title)
	require.True(t, info.IsValid())
}

func mustDoc(t *testing.T, page string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	require.NoError(t, err)
	return doc
}
