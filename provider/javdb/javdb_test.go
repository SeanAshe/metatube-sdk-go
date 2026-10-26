package javdb

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"

	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/testkit"
)

func TestJavDB_ParseDetail(t *testing.T) {
	const page = `
<html><head><title>SSIS-001 | JavDB</title></head><body>
<h2 class="title is-4">
  <strong>SSIS-001 </strong>
  <strong class="current-title">中文標題</strong>
  <span class="origin-title">日本語タイトル</span>
</h2>
<img class="video-cover" src="https://c0.jdbstatic.com/covers/zy/ZY5eq.jpg">
<div class="panel-block"><strong>番號:</strong><span class="value"><a href="/video_codes/SSIS">SSIS</a>-001</span></div>
<div class="panel-block"><strong>日期:</strong><span class="value">2021-02-19</span></div>
<div class="panel-block"><strong>時長:</strong><span class="value">150 分鍾</span></div>
<div class="panel-block"><strong>導演:</strong><span class="value"><a href="/directors/rqk">苺原</a></span></div>
<div class="panel-block"><strong>片商:</strong><span class="value">PRESTIGE,プレステージ</span></div>
<div class="panel-block"><strong>發行:</strong><span class="value"><a href="/publishers/x">S1</a></span></div>
<div class="panel-block"><strong>系列:</strong><span class="value"><a href="/series/z">シリーズ名</a></span></div>
<div class="panel-block"><strong>評分:</strong><span class="value"><span class="score-stars"></span> 4.43分, 由2266人評價</span></div>
<div class="panel-block"><strong>類別:</strong><span class="value"><a href="/tags?c7=11">戲劇</a>, <a href="/tags?c4=91">美乳</a>, <a href="/tags?c4=91">美乳</a></span></div>
<div class="panel-block"><strong>演員:</strong><span class="value">
  <a class="actor-female" href="/actors/A5yq">葵つかさ</a>,
  <a class="actor-female" href="/actors/AGR0">乙白さやか</a>,
  <a href="/actors/8VNb9">平田司</a>
</span></div>
<div class="tile-images preview-images">
  <a class="preview-video-container" href="/login"><img class="video-cover" src="https://c0.jdbstatic.com/covers/zy/ZY5eq.jpg"></a>
  <a class="tile-item" href="https://c0.jdbstatic.com/samples/zy/ZY5eq_l_0.jpg"><img src="https://c0.jdbstatic.com/samples/zy/ZY5eq_s_0.jpg"></a>
  <a class="tile-item" href="https://c0.jdbstatic.com/samples/zy/ZY5eq_l_1.jpg"><img src="https://c0.jdbstatic.com/samples/zy/ZY5eq_s_1.jpg"></a>
</div>
</body></html>`

	info := &model.MovieInfo{
		ID:            "ZY5eq",
		Homepage:      "https://javdb.com/v/ZY5eq",
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}
	doc := mustDoc(t, page)
	require.NoError(t, applyDetail(doc, info))
	require.Equal(t, "日本語タイトル", info.Title)
	require.Equal(t, "SSIS-001", info.Number)
	require.Equal(t, "https://c0.jdbstatic.com/covers/zy/ZY5eq.jpg", info.CoverURL)
	require.Equal(t, "プレステージ", info.Maker)
	require.Equal(t, "苺原", info.Director)
	require.Equal(t, "S1", info.Label)
	require.Equal(t, "シリーズ名", info.Series)
	require.Equal(t, 4.43, info.Score)
	require.Equal(t, 150, info.Runtime)
	require.Equal(t, "2021-02-19", time.Time(info.ReleaseDate).Format("2006-01-02"))
	require.Equal(t, []string{"戲劇", "美乳"}, []string(info.Genres))
	require.Equal(t, []string{"葵つかさ", "乙白さやか"}, []string(info.Actors))
	require.Equal(t, []string{
		"https://c0.jdbstatic.com/samples/zy/ZY5eq_l_0.jpg",
		"https://c0.jdbstatic.com/samples/zy/ZY5eq_l_1.jpg",
	}, []string(info.PreviewImages))
}

func TestJavDB_ParseSearch(t *testing.T) {
	const page = `
<div class="movie-list h cols-4">
  <div class="item">
    <a href="/v/ZY5eq" class="box" title="日本語タイトル">
      <img src="https://c0.jdbstatic.com/covers/zy/ZY5eq.jpg">
      <div class="video-title"><strong>SSIS-001</strong> 日本語タイトル</div>
      <div class="score"><span class="value">4.43分, 由2266人評價</span></div>
      <div class="meta">2021-02-19</div>
    </a>
  </div>
  <a href="/actors/A5yq" class="box actor-box" title="葵つかさ"><strong>葵つかさ</strong></a>
</div>`
	results := parseSearch(mustDoc(t, page), Name)
	require.Len(t, results, 1)
	require.Equal(t, "ZY5eq", results[0].ID)
	require.Equal(t, "SSIS-001", results[0].Number)
	require.Equal(t, "日本語タイトル", results[0].Title)
	require.Equal(t, "https://javdb.com/v/ZY5eq", results[0].Homepage)
	require.Equal(t, 4.43, results[0].Score)
	require.True(t, results[0].IsValid())
}

func TestJavDB_Normalize(t *testing.T) {
	jav := New()
	require.Equal(t, "ZY5eq", jav.NormalizeMovieID("ZY5eq"))
	require.Equal(t, "SSIS-001", jav.NormalizeMovieID("ssis-001"))
	require.Equal(t, "SSIS001", jav.NormalizeMovieID("ssis001"))
	require.Equal(t, "SSIS-001", jav.NormalizeMovieKeyword("ssis001"))
	require.Equal(t, "SSIS-001", jav.NormalizeMovieKeyword("SSIS-001"))
	require.Equal(t, "ZY5eq", jav.NormalizeMovieKeyword("ZY5eq"))
	require.Equal(t, "FC2-1234567", jav.NormalizeMovieKeyword("fc2-1234567"))
	require.False(t, isSlug("SSIS001"))
	require.True(t, isSlug("ZY5eq"))
	require.True(t, isSlug("OM9O8"))
	require.Equal(t, "SSIS001", compactCode("ssis-001"))

	id, err := jav.ParseMovieIDFromURL("https://javdb.com/v/ZY5eq?locale=zh")
	require.NoError(t, err)
	require.Equal(t, "ZY5eq", id)
	_, err = jav.ParseMovieIDFromURL("https://javdb.com/search?q=SSIS-001")
	require.Error(t, err)
}

func TestJavDB_GetMovieInfoByID(t *testing.T) {
	testkit.Test(t, New, []string{
		"ZY5eq",
		"SSIS-001",
	})
}

func TestJavDB_SearchMovie(t *testing.T) {
	testkit.Test(t, New, []string{
		"SSIS-001",
		"ssis001",
	})
}

func mustDoc(t *testing.T, page string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	require.NoError(t, err)
	return doc
}
