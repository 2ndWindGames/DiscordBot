package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseAppBrainChart(t *testing.T) {
	html := `<table><tbody><tr>
		<td class="ranking-rank">1</td><td class="ranking-equal">=</td><td class="ranking-icon-cell"></td>
		<td class="ranking-app-cell"><a href="/app/sample/com.example.game">Sample Puzzle</a><div class="ranking-app-cell-creator">by <a>Example</a></div></td>
		<td class="ranking-rating-cell"><span>4.7</span></td><td>5.5 M</td><td>120 K</td>
	</tr></tbody></table>`
	response := &http.Response{Body: io.NopCloser(strings.NewReader(html))}

	games, err := parseAppBrainChart(response, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].Name != "Sample Puzzle" {
		t.Fatalf("unexpected games: %#v", games)
	}
	if games[0].Installs != 5_500_000 || games[0].RecentInstalls != 120_000 {
		t.Fatalf("unexpected estimates: %#v", games[0])
	}
}

func TestParseCompactNumber(t *testing.T) {
	tests := map[string]int64{"1.1 B": 1_100_000_000, "5.5 M": 5_500_000, "120 K": 120_000, "42": 42}
	for input, want := range tests {
		if got := parseCompactNumber(input); got != want {
			t.Errorf("parseCompactNumber(%q)=%d, want %d", input, got, want)
		}
	}
}

func TestMissingEstimatesAreNotDisplayedAsZero(t *testing.T) {
	if got := formatRating(0); got != "정보 없음" {
		t.Fatalf("formatRating(0)=%q", got)
	}
	if got := formatEstimate(0); got != "정보 없음" {
		t.Fatalf("formatEstimate(0)=%q", got)
	}
	if got := formatDailyEstimate(0); got != "계산 불가" {
		t.Fatalf("formatDailyEstimate(0)=%q", got)
	}
}

func TestFetchGooglePlayDetailsFields(t *testing.T) {
	if got := formatRatingCount(781); got != "평가 781개" {
		t.Fatalf("formatRatingCount(781)=%q", got)
	}
	if got := formatDownloadRange("10만+"); got != "10만+" {
		t.Fatalf("formatDownloadRange=%q", got)
	}
}
