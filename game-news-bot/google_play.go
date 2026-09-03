package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type PlayGame struct {
	Rank           int     `json:"rank"`
	Name           string  `json:"name"`
	Developer      string  `json:"developer,omitempty"`
	URL            string  `json:"url"`
	Rating         float64 `json:"rating,omitempty"`
	RatingCount    int64   `json:"rating_count,omitempty"`
	DownloadRange  string  `json:"download_range,omitempty"`
	Installs       int64   `json:"installs,omitempty"`
	RecentInstalls int64   `json:"recent_installs,omitempty"`
}

type googlePlayStructuredData struct {
	AggregateRating struct {
		RatingValue string `json:"ratingValue"`
		RatingCount string `json:"ratingCount"`
	} `json:"aggregateRating"`
}

type PlayChart struct {
	Key         string
	Title       string
	Description string
	URL         string
	Games       []PlayGame
}

type PlaySnapshot struct {
	CollectedAt time.Time             `json:"collected_at"`
	Charts      map[string][]PlayGame `json:"charts"`
}

var playChartDefinitions = []struct {
	Key         string
	Title       string
	Description string
}{
	{"top_free", "인기 무료 게임", "Google Play 무료 게임 순위"},
	{"top_paid", "인기 유료 게임", "Google Play 유료 다운로드 순위"},
	{"top_grossing", "최고 매출 게임", "Google Play 인앱결제 포함 매출 순위"},
	{"top_new_free", "신규 인기 무료 게임", "신규 무료 게임 인기 순위"},
	{"top_new_paid", "신규 인기 유료 게임", "신규 유료 게임 인기 순위"},
}

func runGooglePlayBot(c GooglePlayConfig) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		log.Printf("❌ Google Play timezone: %v", err)
		return
	}
	log.Printf("🎯 Google Play Trend Bot started (daily %s %s)", c.RunAt, c.Timezone)
	for {
		next, err := nextDailyRun(time.Now(), c.RunAt, loc)
		if err != nil {
			log.Printf("❌ Google Play schedule: %v", err)
			return
		}
		log.Printf("⏰ next Google Play report: %s", next.Format(time.RFC3339))
		time.Sleep(time.Until(next))
		if err := sendGooglePlayReport(c); err != nil {
			log.Printf("❌ Google Play report: %v", err)
		}
	}
}

func nextDailyRun(now time.Time, runAt string, loc *time.Location) (time.Time, error) {
	parsed, err := time.Parse("15:04", runAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("run_at must be HH:MM: %w", err)
	}
	n := now.In(loc)
	next := time.Date(n.Year(), n.Month(), n.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
	if !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func sendGooglePlayReport(c GooglePlayConfig) error {
	previous, _ := loadPlaySnapshot(c.StateFile)
	charts, err := fetchPlayCharts(c)
	if err != nil {
		return err
	}
	enrichPlayCharts(charts, c)

	collectedAt := time.Now()
	for _, chart := range charts {
		payload := buildChartPayload(chart, previous.Charts[chart.Key], collectedAt)
		if err := postDiscordToAll(c.WebhookURLs(), payload); err != nil {
			log.Printf("❌ [%s] Discord send: %v", chart.Title, err)
		}
		time.Sleep(900 * time.Millisecond)
	}

	if err := postDiscordToAll(c.WebhookURLs(), buildInsightPayload(charts, collectedAt)); err != nil {
		log.Printf("❌ Google Play insight send: %v", err)
	}

	snapshot := PlaySnapshot{CollectedAt: collectedAt, Charts: make(map[string][]PlayGame)}
	for _, chart := range charts {
		snapshot.Charts[chart.Key] = chart.Games
	}
	return savePlaySnapshot(c.StateFile, snapshot)
}

func fetchPlayCharts(c GooglePlayConfig) ([]PlayChart, error) {
	charts := make([]PlayChart, 0, len(playChartDefinitions))
	for _, definition := range playChartDefinitions {
		chartURL := appBrainChartURL(definition.Key, c.Country)
		games, err := fetchAppBrainChart(chartURL, c.TopCount)
		if err != nil {
			log.Printf("❌ [%s] collect failed: %v", definition.Title, err)
			continue
		}
		charts = append(charts, PlayChart{Key: definition.Key, Title: definition.Title, Description: definition.Description, URL: chartURL, Games: games})
		time.Sleep(1500 * time.Millisecond)
	}
	if len(charts) == 0 {
		return nil, fmt.Errorf("all Google Play chart collections failed")
	}
	return charts, nil
}

func enrichPlayCharts(charts []PlayChart, c GooglePlayConfig) {
	cache := make(map[string]PlayGame)
	for chartIndex := range charts {
		for gameIndex := range charts[chartIndex].Games {
			game := &charts[chartIndex].Games[gameIndex]
			if details, ok := cache[game.URL]; ok {
				applyPlayDetails(game, details)
				continue
			}
			details, err := fetchGooglePlayDetails(game.URL, c.Language, c.Country)
			if err != nil {
				log.Printf("⚠️ [%s] details unavailable: %v", game.Name, err)
				continue
			}
			cache[game.URL] = details
			applyPlayDetails(game, details)
			time.Sleep(250 * time.Millisecond)
		}
	}
}

func applyPlayDetails(game *PlayGame, details PlayGame) {
	if details.Rating > 0 {
		game.Rating = details.Rating
	}
	game.RatingCount = details.RatingCount
	game.DownloadRange = details.DownloadRange
}

func fetchGooglePlayDetails(gameURL, language, country string) (PlayGame, error) {
	parsedURL, err := url.Parse(gameURL)
	if err != nil {
		return PlayGame{}, err
	}
	query := parsedURL.Query()
	query.Set("hl", language)
	query.Set("gl", country)
	parsedURL.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return PlayGame{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GameTrendBot/1.0)")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return PlayGame{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PlayGame{}, fmt.Errorf("Google Play details response: %s", resp.Status)
	}

	document, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return PlayGame{}, err
	}
	var structured googlePlayStructuredData
	document.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		return json.Unmarshal([]byte(selection.Text()), &structured) != nil
	})

	rating, _ := strconv.ParseFloat(structured.AggregateRating.RatingValue, 64)
	ratingCount, _ := strconv.ParseInt(structured.AggregateRating.RatingCount, 10, 64)
	downloadRange := ""
	document.Find(".wVqUob").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		if strings.TrimSpace(selection.Find(".g1rdde").Text()) != "다운로드" {
			return true
		}
		downloadRange = strings.TrimSpace(selection.Find(".ClM7O").Text())
		return false
	})
	return PlayGame{Rating: rating, RatingCount: ratingCount, DownloadRange: downloadRange}, nil
}

func appBrainChartURL(chartKey, country string) string {
	country = strings.ToLower(country)
	return fmt.Sprintf("https://www.appbrain.com/stats/google-play-rankings/%s/game/%s", chartKey, url.PathEscape(country))
}

func fetchAppBrainChart(chartURL string, topCount int) ([]PlayGame, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodGet, chartURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GameTrendBot/1.0)")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			resp.Body.Close()
			time.Sleep(10 * time.Second)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("chart response: %s", resp.Status)
		}
		return parseAppBrainChart(resp, topCount)
	}
	return nil, fmt.Errorf("chart request retry exhausted")
}

func parseAppBrainChart(resp *http.Response, topCount int) ([]PlayGame, error) {
	document, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse chart HTML: %w", err)
	}

	var games []PlayGame
	headerIndex := make(map[string]int)
	document.Find("table thead th").Each(func(index int, header *goquery.Selection) {
		headerIndex[strings.ToLower(strings.TrimSpace(header.Text()))] = index
	})
	document.Find("table tbody tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		rank, err := strconv.Atoi(strings.TrimSpace(row.Find(".ranking-rank").Text()))
		if err != nil {
			return true
		}
		appLink := row.Find(".ranking-app-cell > a").First()
		name := strings.TrimSpace(appLink.Text())
		href, _ := appLink.Attr("href")
		if name == "" || href == "" {
			return true
		}
		developer := strings.TrimSpace(row.Find(".ranking-app-cell-creator a").First().Text())
		rating, _ := strconv.ParseFloat(strings.TrimSpace(row.Find(".ranking-rating-cell span").Last().Text()), 64)

		cells := row.Find("td")
		installIndex, hasInstallHeader := headerIndex["installs"]
		recentIndex, hasRecentHeader := headerIndex["recent"]
		var installs, recent int64
		if hasInstallHeader && hasRecentHeader {
			installs = parseCompactNumber(strings.TrimSpace(cells.Eq(installIndex).Text()))
			recent = parseCompactNumber(strings.TrimSpace(cells.Eq(recentIndex).Text()))
		} else {
			plainCells := cells.FilterFunction(func(_ int, cell *goquery.Selection) bool {
				class, _ := cell.Attr("class")
				return strings.TrimSpace(class) == ""
			})
			installs = parseCompactNumber(strings.TrimSpace(plainCells.Eq(0).Text()))
			recent = parseCompactNumber(strings.TrimSpace(plainCells.Eq(1).Text()))
		}

		packageName := href[strings.LastIndex(href, "/")+1:]
		gameURL := "https://play.google.com/store/apps/details?id=" + url.QueryEscape(packageName)
		games = append(games, PlayGame{Rank: rank, Name: name, Developer: developer, URL: gameURL, Rating: rating, Installs: installs, RecentInstalls: recent})
		return len(games) < topCount
	})
	if len(games) == 0 {
		return nil, fmt.Errorf("no ranked games found; chart page structure may have changed")
	}
	return games, nil
}

func parseCompactNumber(value string) int64 {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" || value == "-" {
		return 0
	}
	multiplier := float64(1)
	suffix := strings.ToUpper(value[len(value)-1:])
	switch suffix {
	case "K":
		multiplier = 1_000
	case "M":
		multiplier = 1_000_000
	case "B":
		multiplier = 1_000_000_000
	default:
		suffix = ""
	}
	if suffix != "" {
		value = strings.TrimSpace(value[:len(value)-1])
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(number * multiplier)
}

func buildChartPayload(chart PlayChart, previous []PlayGame, collectedAt time.Time) DiscordPayload {
	oldRank := make(map[string]int)
	for _, game := range previous {
		oldRank[game.URL] = game.Rank
	}
	lines := make([]string, 0, len(chart.Games))
	for _, game := range chart.Games {
		movement := "NEW"
		if old, ok := oldRank[game.URL]; ok {
			difference := old - game.Rank
			switch {
			case difference > 0:
				movement = fmt.Sprintf("▲%d", difference)
			case difference < 0:
				movement = fmt.Sprintf("▼%d", -difference)
			default:
				movement = "－"
			}
		}
		line := fmt.Sprintf(
			"**%d.** [%s](%s) `%s`\n└ ⭐ %s (%s) · Play 다운로드 %s",
			game.Rank,
			game.Name,
			game.URL,
			movement,
			formatRating(game.Rating),
			formatRatingCount(game.RatingCount),
			formatDownloadRange(game.DownloadRange),
		)
		lines = append(lines, line)
	}
	description := chart.Description + "\n" + strings.Join(lines, "\n")
	return DiscordPayload{Username: "🎯 Google Play Game Trends", Content: "📊 **" + chart.Title + "**", Embeds: []DiscordEmbed{{Title: chart.Title, URL: chart.URL, Description: truncate(description, 4000), Color: chartColor(chart.Key), Footer: &DiscordFooter{Text: "순위: Google Play 기반 일일 수집 · 다운로드: Google Play 공개 구간"}, Timestamp: collectedAt.Format(time.RFC3339)}}}
}

func buildInsightPayload(charts []PlayChart, collectedAt time.Time) DiscordPayload {
	chartRanks := make(map[string]map[string]int)
	gameByURL := make(map[string]PlayGame)
	for _, chart := range charts {
		for _, game := range chart.Games {
			gameByURL[game.URL] = game
			if chartRanks[game.URL] == nil {
				chartRanks[game.URL] = make(map[string]int)
			}
			chartRanks[game.URL][chart.Key] = game.Rank
		}
	}

	benchmark, score := selectLightTeamBenchmark(charts, chartRanks)
	overlaps := make([]string, 0)
	for gameURL, ranks := range chartRanks {
		if len(ranks) >= 2 {
			overlaps = append(overlaps, fmt.Sprintf("• **%s**: %d개 차트 동시 진입", gameByURL[gameURL].Name, len(ranks)))
		}
		if len(overlaps) == 4 {
			break
		}
	}
	if len(overlaps) == 0 {
		overlaps = append(overlaps, "• TOP 목록 내 차트 교차 진입 게임 없음")
	}

	conclusion := "데이터가 부족해 오늘의 제작 후보를 선정하지 못했습니다."
	if benchmark.Name != "" {
		conclusion = fmt.Sprintf("**오늘의 벤치마킹 후보: [%s](%s)** (소규모 제작 적합도 %d/100)\n완제품을 복제하기보다 1회 플레이 핵심 루프, 세션 길이, 광고/IAP 위치를 분해해 2~4주 프로토타입으로 검증하세요. 아트·IP·레벨은 독자적으로 설계해야 합니다.", benchmark.Name, benchmark.URL, score)
	}
	description := "**시장 신호**\n" + strings.Join(overlaps, "\n") + "\n\n**제작팀 결론**\n" + conclusion + "\n\n**해석 주의**\n설치 수는 제3자 추정치이며, 매출액과 Google의 순위 산정 기간은 공개되지 않습니다. 최고 매출 순위는 절대 매출이 아닌 상대적 수익화 신호로만 사용했습니다."
	return DiscordPayload{Username: "🎯 Google Play Game Trends", Content: "🧭 **라이트 게임 제작팀용 오늘의 결론**", Embeds: []DiscordEmbed{{Title: "차트 종합 분석", Description: truncate(description, 4000), Color: 0xF9AB00, Footer: &DiscordFooter{Text: "휴리스틱 분석 · 투자/매출 보장 아님"}, Timestamp: collectedAt.Format(time.RFC3339)}}}
}

func selectLightTeamBenchmark(charts []PlayChart, chartRanks map[string]map[string]int) (PlayGame, int) {
	var selected PlayGame
	bestScore := -1
	lightKeywords := []string{"puzzle", "퍼즐", "sort", "정렬", "idle", "방치", "merge", "매치", "quiz", "퀴즈", "sudoku", "스도쿠", "block", "블록", "tap", "타일", "2048"}
	heavyKeywords := []string{"mmo", "리니지", "4x", "strategy", "전략", "rpg", "오픈월드", "sports", "야구", "football"}
	for _, chart := range charts {
		if chart.Key != "top_free" && chart.Key != "top_new_free" {
			continue
		}
		for _, game := range chart.Games {
			name := strings.ToLower(game.Name)
			score := 45 + int(math.Max(0, 25-float64(game.Rank)*2))
			for _, keyword := range lightKeywords {
				if strings.Contains(name, keyword) {
					score += 15
					break
				}
			}
			for _, keyword := range heavyKeywords {
				if strings.Contains(name, keyword) {
					score -= 30
					break
				}
			}
			if len(chartRanks[game.URL]) >= 2 {
				score += 10
			}
			if game.RecentInstalls >= 100_000 {
				score += 5
			}
			if score > 100 {
				score = 100
			}
			if score > bestScore {
				selected, bestScore = game, score
			}
		}
	}
	return selected, bestScore
}

func formatCompactNumber(value int64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return strconv.FormatInt(value, 10)
	}
}

func formatRating(value float64) string {
	if value <= 0 {
		return "정보 없음"
	}
	return fmt.Sprintf("%.1f", value)
}

func formatRatingCount(value int64) string {
	if value <= 0 {
		return "평가 수 정보 없음"
	}
	return "평가 " + formatCompactNumber(value) + "개"
}

func formatDownloadRange(value string) string {
	if strings.TrimSpace(value) == "" {
		return "정보 없음"
	}
	return value
}

func formatEstimate(value int64) string {
	if value <= 0 {
		return "정보 없음"
	}
	return "≈" + formatCompactNumber(value)
}

func formatDailyEstimate(recentInstalls int64) string {
	if recentInstalls <= 0 {
		return "계산 불가"
	}
	return "≈" + formatCompactNumber(recentInstalls/30)
}

func chartColor(key string) int {
	switch key {
	case "top_free":
		return 0x34A853
	case "top_paid":
		return 0x4285F4
	case "top_grossing":
		return 0xFBBC04
	default:
		return 0xA142F4
	}
}

func loadPlaySnapshot(path string) (PlaySnapshot, error) {
	var snapshot PlaySnapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(b, &snapshot)
	return snapshot, err
}

func savePlaySnapshot(path string, snapshot PlaySnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
