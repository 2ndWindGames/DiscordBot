package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
)

const (
	// 사용자가 전달한 Discord Webhook
	discordWebhookURL = "https://discord.com/api/webhooks/1543958659300720680/7yB0TTxVwHMlocd_7kSVeEu4rVbV6PwHllMyqPtTtHwo9ksSPG6hnP0j9SI90ieyNKWe"

	// 뉴스 확인 주기
	checkInterval = 180 * time.Minute

	// 프로그램 시작 시 보여줄 최신 기사 개수
	initialArticleCount = 5

	// 한 번 체크할 때 최대 전송 개수
	maxArticlesPerCheck = 5

	// HTTP Timeout
	httpTimeout = 10 * time.Second
)

type Feed struct {
	Name     string
	URL      string
	Category string
	Emoji    string

	// true면 RSS 자체가 검색 결과이므로
	// 키워드 필터를 조금 느슨하게 적용
	SearchFeed bool
}

type Article struct {
	Feed     Feed
	Item     *gofeed.Item
	Category string
	Emoji    string
	Priority int
	Time     time.Time
}

type DiscordPayload struct {
	Username string         `json:"username,omitempty"`
	Content  string         `json:"content,omitempty"`
	Embeds   []DiscordEmbed `json:"embeds,omitempty"`
}

type DiscordEmbed struct {
	Title       string         `json:"title"`
	URL         string         `json:"url,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []DiscordField `json:"fields,omitempty"`
	Footer      *DiscordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type DiscordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordFooter struct {
	Text string `json:"text"`
}

var (
	seen   = make(map[string]bool)
	seenMu sync.Mutex
)

/*
취업 관련 키워드

서버 개발자를 포함해서 개발 직군 관련 키워드를
조금 넓게 잡음.
*/
var jobKeywords = []string{
	"채용",
	"공채",
	"신입",
	"경력",
	"인턴",
	"인턴십",
	"취업",
	"구인",
	"모집",
	"개발자 모집",
	"인재 모집",
	"인재 채용",
	"게임 개발자",
	"게임 프로그래머",
	"서버 개발자",
	"클라이언트 개발자",
	"백엔드 개발자",
	"프로그래머 채용",
	"프로그래머 모집",
	"채용 설명회",
	"채용연계",
}

/*
게임업계에 꽤 중요한 위험 신호들
*/
var alertKeywords = []string{
	"구조조정",
	"권고사직",
	"희망퇴직",
	"명예퇴직",
	"감원",
	"인력 감축",
	"인력감축",
	"해고",
	"정리해고",
	"폐업",
	"철수",
	"스튜디오 폐쇄",
	"서비스 종료",
	"사업 철수",
	"프로젝트 중단",
	"개발 중단",
	"개발 취소",
	"프로젝트 취소",
}

/*
산업 동향
*/
var industryKeywords = []string{
	"게임업계",
	"게임 산업",
	"게임산업",
	"게임사",
	"게임 회사",
	"게임회사",

	"투자",
	"투자유치",
	"인수",
	"합병",
	"m&a",

	"실적",
	"매출",
	"영업이익",
	"영업손실",
	"적자",
	"흑자",
	"순이익",

	"조직개편",
	"신규 법인",
	"자회사",
	"스튜디오 설립",

	"퍼블리싱",
	"퍼블리셔",
}

/*
주요 국내 게임 회사

회사 관련 뉴스라면 일반 게임 콘텐츠 뉴스보다
조금 더 관심 있게 본다.
*/
var companyKeywords = []string{
	"넥슨",
	"nexon",

	"엔씨소프트",
	"엔씨",
	"ncsoft",

	"크래프톤",
	"krafton",

	"넷마블",

	"스마일게이트",

	"펄어비스",

	"네오위즈",

	"위메이드",

	"카카오게임즈",

	"컴투스",

	"컴투스홀딩스",

	"웹젠",

	"조이시티",

	"데브시스터즈",

	"시프트업",

	"하이브im",

	"라인게임즈",

	"NHN",
}

/*
우리가 별로 받고 싶지 않은 일반 게임 뉴스
*/
var ignoreKeywords = []string{
	"업데이트 실시",
	"신규 캐릭터",
	"신규 캐릭터 출시",
	"신규 스킨",
	"이벤트 실시",
	"이벤트 개최",
	"콜라보",
	"콜라보레이션",
	"사전예약",
	"사전 예약",
	"쿠폰",
	"신규 던전",
	"신규 보스",
	"신규 시즌",
}

/*
기본 RSS 목록
*/
var feeds = []Feed{
	{
		Name:       "인벤",
		URL:        "http://feeds.feedburner.com/inven",
		Category:   "국내 게임업계",
		Emoji:      "🇰🇷",
		SearchFeed: false,
	},
	{
		Name:       "게임동아",
		URL:        "https://game.donga.com/feeds/rss/news/",
		Category:   "국내 게임업계",
		Emoji:      "🇰🇷",
		SearchFeed: false,
	},

	/*
		Google News 검색 RSS

		여러 국내 언론사 기사를 한 번에 검색한다.
	*/
	{
		Name: "게임업계 채용",
		URL: googleNewsRSS(
			`"게임 개발자" 채용 OR "게임회사" 채용 OR "게임사" 채용 OR 게임 공채 OR 게임 인턴`,
		),
		Category:   "채용",
		Emoji:      "💼",
		SearchFeed: true,
	},
	{
		Name: "게임 개발자 취업",
		URL: googleNewsRSS(
			`게임 개발자 취업 OR 게임 프로그래머 채용 OR 게임 서버 개발자 채용 OR 게임 클라이언트 개발자 채용`,
		),
		Category:   "채용",
		Emoji:      "👨‍💻",
		SearchFeed: true,
	},
	{
		Name: "게임업계 구조조정",
		URL: googleNewsRSS(
			`게임업계 구조조정 OR 게임사 권고사직 OR 게임회사 희망퇴직 OR 게임사 감원 OR 게임 프로젝트 중단`,
		),
		Category:   "업계 경보",
		Emoji:      "🚨",
		SearchFeed: true,
	},
	{
		Name: "게임업계 투자",
		URL: googleNewsRSS(
			`게임업계 투자 OR 게임사 투자 OR 게임회사 인수 OR 게임회사 합병 OR 게임사 실적`,
		),
		Category:   "산업 동향",
		Emoji:      "📊",
		SearchFeed: true,
	},
	{
		Name: "주요 게임사 채용",
		URL: googleNewsRSS(
			`(넥슨 OR 엔씨소프트 OR 크래프톤 OR 넷마블 OR 스마일게이트 OR 펄어비스 OR 네오위즈 OR 시프트업) 채용`,
		),
		Category:   "채용",
		Emoji:      "🏢",
		SearchFeed: true,
	},
}

func main() {
	log.Println("🎮 Korea Game Industry Bot started")
	log.Printf("📰 feeds: %d", len(feeds))
	log.Printf("⏱️ interval: %s", checkInterval)

	initializeFeeds()

	sendSystemMessage(
		"🟢 **Korea Game Industry Bot Started**\n" +
			"🇰🇷 국내 게임업계 / 💼 채용 / 🚨 구조조정 / 📊 산업동향을 모니터링합니다.",
	)

	log.Println("👀 Waiting for new articles...")

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("")
		log.Println("🔍 Checking feeds...")

		checkAllFeeds()

		log.Println("😴 Waiting for next check...")
	}
}

func googleNewsRSS(query string) string {
	return "https://news.google.com/rss/search?q=" +
		url.QueryEscape(query) +
		"&hl=ko&gl=KR&ceid=KR:ko"
}

func newParser() *gofeed.Parser {
	parser := gofeed.NewParser()

	parser.Client = &http.Client{
		Timeout: httpTimeout,
	}

	return parser
}

/*
처음 실행할 때:

1. 전체 RSS 로딩
2. 우리가 원하는 기사만 선별
3. 최신순 정렬
4. 최신 5개 Discord 전송
5. 현재 기사들은 모두 seen 처리
*/
func initializeFeeds() {
	log.Println("")
	log.Println("🚀 Initializing feeds...")

	var articles []Article

	articleMap := make(map[string]bool)

	for _, feed := range feeds {
		parser := newParser()

		result, err := parser.ParseURL(feed.URL)
		if err != nil {
			log.Printf(
				"❌ [%s] initialize failed: %v",
				feed.Name,
				err,
			)
			continue
		}

		matchCount := 0

		for _, item := range result.Items {
			id := articleID(item)

			/*
				초기 RSS에 존재하는 기사는
				이후 체크에서 중복 발송하지 않도록 등록
			*/
			markSeen(id)

			article, ok := createArticle(feed, item)
			if !ok {
				continue
			}

			/*
				검색 RSS 여러 개에서 같은 기사가
				잡히는 경우가 있으므로 한 번만 추가
			*/
			if articleMap[id] {
				continue
			}

			articleMap[id] = true
			articles = append(articles, article)

			matchCount++
		}

		log.Printf(
			"✅ [%s] total=%d / relevant=%d",
			feed.Name,
			len(result.Items),
			matchCount,
		)
	}

	if len(articles) == 0 {
		log.Println("⚠️ No relevant articles found")
		return
	}

	sort.Slice(articles, func(i, j int) bool {
		if articles[i].Priority != articles[j].Priority {
			return articles[i].Priority > articles[j].Priority
		}

		return articles[i].Time.After(articles[j].Time)
	})

	/*
		초기에는 Priority보다 정말 최신 뉴스가
		위에 오는 것이 자연스러우므로 최종적으로 시간순 정렬
	*/
	sort.SliceStable(articles, func(i, j int) bool {
		return articles[i].Time.After(articles[j].Time)
	})

	limit := initialArticleCount

	if len(articles) < limit {
		limit = len(articles)
	}

	log.Printf("")
	log.Printf(
		"📨 Sending latest %d relevant articles...",
		limit,
	)

	/*
		Discord에서는 가장 최근 기사가 아래쪽에
		보이도록 오래된 순서부터 보냄
	*/
	for i := limit - 1; i >= 0; i-- {
		article := articles[i]

		if err := sendArticle(article); err != nil {
			log.Printf(
				"❌ [%s] initial send failed: %v",
				article.Feed.Name,
				err,
			)
			continue
		}

		log.Printf(
			"📨 [%s][%s] %s",
			article.Feed.Name,
			article.Category,
			article.Item.Title,
		)

		time.Sleep(900 * time.Millisecond)
	}
}

/*
3분마다 전체 RSS를 병렬 체크
*/
func checkAllFeeds() {
	var wg sync.WaitGroup

	for _, feed := range feeds {
		wg.Add(1)

		go func(feed Feed) {
			defer wg.Done()

			checkFeed(feed)
		}(feed)
	}

	wg.Wait()
}

func checkFeed(feed Feed) {
	parser := newParser()

	result, err := parser.ParseURL(feed.URL)
	if err != nil {
		log.Printf(
			"❌ [%s] RSS error: %v",
			feed.Name,
			err,
		)
		return
	}

	var newArticles []Article

	for _, item := range result.Items {
		id := articleID(item)

		if isSeen(id) {
			continue
		}

		/*
			새 기사라고 판단했으면 일단 seen 처리.

			우리 필터에서 탈락했다고 해서
			다음 3분마다 계속 검사할 필요는 없음.
		*/
		markSeen(id)

		article, ok := createArticle(feed, item)
		if !ok {
			continue
		}

		newArticles = append(
			newArticles,
			article,
		)
	}

	log.Printf(
		"🔎 [%s] total=%d / new-important=%d",
		feed.Name,
		len(result.Items),
		len(newArticles),
	)

	if len(newArticles) == 0 {
		return
	}

	/*
		시간순
	*/
	sort.Slice(newArticles, func(i, j int) bool {
		return newArticles[i].Time.Before(
			newArticles[j].Time,
		)
	})

	/*
		한 RSS에서 갑자기 수십 개가 생겨도
		Discord 도배 방지
	*/
	if len(newArticles) > maxArticlesPerCheck {
		newArticles =
			newArticles[len(newArticles)-maxArticlesPerCheck:]
	}

	for _, article := range newArticles {
		if err := sendArticle(article); err != nil {
			log.Printf(
				"❌ [%s] Discord send failed: %v",
				feed.Name,
				err,
			)
			continue
		}

		log.Printf(
			"📰 [%s][%s] %s",
			feed.Name,
			article.Category,
			article.Item.Title,
		)

		time.Sleep(900 * time.Millisecond)
	}
}

func createArticle(
	feed Feed,
	item *gofeed.Item,
) (Article, bool) {

	text := strings.ToLower(
		item.Title +
			" " +
			item.Description,
	)

	/*
		분류 우선순위

		업계 경보 > 채용 > 산업동향
	*/

	if containsAny(text, alertKeywords) {
		return Article{
			Feed:     feed,
			Item:     item,
			Category: "업계 경보",
			Emoji:    "🚨",
			Priority: 100,
			Time:     publishedTime(item),
		}, true
	}

	if containsAny(text, jobKeywords) {
		return Article{
			Feed:     feed,
			Item:     item,
			Category: "채용 / 취업",
			Emoji:    "💼",
			Priority: 90,
			Time:     publishedTime(item),
		}, true
	}

	/*
		Google News에서 아예 산업 검색으로 가져온 것은
		산업뉴스로 인정
	*/
	if feed.Category == "산업 동향" {
		return Article{
			Feed:     feed,
			Item:     item,
			Category: "산업 동향",
			Emoji:    "📊",
			Priority: 70,
			Time:     publishedTime(item),
		}, true
	}

	if containsAny(text, industryKeywords) {
		/*
			단순 게임 업데이트 뉴스 제외
		*/
		if containsAny(text, ignoreKeywords) &&
			!containsAny(text, companyKeywords) {

			return Article{}, false
		}

		return Article{
			Feed:     feed,
			Item:     item,
			Category: "산업 동향",
			Emoji:    "📊",
			Priority: 60,
			Time:     publishedTime(item),
		}, true
	}

	/*
		검색 RSS 자체가 채용/구조조정 검색인 경우
	*/
	if feed.SearchFeed {
		switch feed.Category {

		case "채용":
			return Article{
				Feed:     feed,
				Item:     item,
				Category: "채용 / 취업",
				Emoji:    "💼",
				Priority: 80,
				Time:     publishedTime(item),
			}, true

		case "업계 경보":
			return Article{
				Feed:     feed,
				Item:     item,
				Category: "업계 경보",
				Emoji:    "🚨",
				Priority: 95,
				Time:     publishedTime(item),
			}, true
		}
	}

	return Article{}, false
}

func sendArticle(article Article) error {
	item := article.Item

	description := cleanText(item.Description)

	if description == "" {
		description =
			"기사 내용을 확인하려면 제목을 클릭하세요."
	}

	description = truncate(description, 450)

	company := detectCompany(
		item.Title + " " + item.Description,
	)

	fields := []DiscordField{
		{
			Name:   "🏷️ 분류",
			Value:  article.Category,
			Inline: true,
		},
		{
			Name:   "📰 출처",
			Value:  article.Feed.Name,
			Inline: true,
		},
	}

	if company != "" {
		fields = append(
			fields,
			DiscordField{
				Name:   "🏢 관련 회사",
				Value:  company,
				Inline: true,
			},
		)
	}

	/*
		기사에서 관심 키워드 찾아서 표시
	*/
	keywords := detectKeywords(
		item.Title + " " + item.Description,
	)

	if len(keywords) > 0 {
		fields = append(
			fields,
			DiscordField{
				Name: "🔎 주요 키워드",
				Value: strings.Join(
					keywords,
					" · ",
				),
				Inline: false,
			},
		)
	}

	embed := DiscordEmbed{
		Title:       truncate(item.Title, 240),
		URL:         item.Link,
		Description: description,
		Color:       categoryColor(article.Category),
		Fields:      fields,

		Footer: &DiscordFooter{
			Text: "SecondWind • Korea Game Industry",
		},

		Timestamp: publishedTime(item).
			Format(time.RFC3339),
	}

	content := fmt.Sprintf(
		"%s **%s**",
		article.Emoji,
		discordHeadline(article.Category),
	)

	payload := DiscordPayload{
		Username: "🎮 Korea Game Industry",
		Content:  content,
		Embeds: []DiscordEmbed{
			embed,
		},
	}

	return postDiscord(payload)
}

func discordHeadline(category string) string {
	switch category {

	case "채용 / 취업":
		return "게임업계 채용 소식"

	case "업계 경보":
		return "게임업계 주요 이슈"

	case "산업 동향":
		return "한국 게임업계 동향"

	default:
		return "게임업계 뉴스"
	}
}

func postDiscord(
	payload DiscordPayload,
) error {

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		discordWebhookURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	client := &http.Client{
		Timeout: httpTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 ||
		resp.StatusCode >= 300 {

		return fmt.Errorf(
			"discord response: %s",
			resp.Status,
		)
	}

	return nil
}

func sendSystemMessage(
	content string,
) {

	payload := DiscordPayload{
		Username: "🎮 Korea Game Industry",
		Content:  content,
	}

	if err := postDiscord(payload); err != nil {
		log.Printf(
			"❌ system message failed: %v",
			err,
		)
	}
}

func articleID(
	item *gofeed.Item,
) string {

	/*
		Google News RSS 검색어가 다르면
		동일 기사가 여러 검색 결과에 포함될 수 있어서

		URL보다 제목 중심으로 중복 판단.
	*/

	title := normalizeArticleTitle(
		item.Title,
	)

	value := strings.ToLower(title)

	if value == "" {
		value = item.GUID
	}

	if value == "" {
		value = item.Link
	}

	hash := sha256.Sum256(
		[]byte(value),
	)

	return hex.EncodeToString(
		hash[:],
	)
}

func normalizeArticleTitle(
	title string,
) string {

	title = html.UnescapeString(title)

	title = strings.TrimSpace(title)

	/*
		Google News 제목:

		넥슨 신입 채용 시작 - 게임메카

		처럼 뒤에 언론사명이 붙는 경우가 많음.
	*/

	parts := strings.Split(title, " - ")

	if len(parts) > 1 {
		title = strings.Join(
			parts[:len(parts)-1],
			" - ",
		)
	}

	title = strings.Join(
		strings.Fields(title),
		" ",
	)

	return title
}

func containsAny(
	text string,
	keywords []string,
) bool {

	text = strings.ToLower(text)

	for _, keyword := range keywords {
		if strings.Contains(
			text,
			strings.ToLower(keyword),
		) {
			return true
		}
	}

	return false
}

func detectCompany(
	text string,
) string {

	textLower := strings.ToLower(text)

	companies := []struct {
		Search string
		Name   string
	}{
		{"넥슨", "넥슨"},
		{"nexon", "넥슨"},

		{"엔씨소프트", "엔씨소프트"},
		{"ncsoft", "엔씨소프트"},

		{"크래프톤", "크래프톤"},
		{"krafton", "크래프톤"},

		{"넷마블", "넷마블"},
		{"스마일게이트", "스마일게이트"},
		{"펄어비스", "펄어비스"},
		{"네오위즈", "네오위즈"},
		{"위메이드", "위메이드"},
		{"카카오게임즈", "카카오게임즈"},
		{"컴투스", "컴투스"},
		{"웹젠", "웹젠"},
		{"조이시티", "조이시티"},
		{"데브시스터즈", "데브시스터즈"},
		{"시프트업", "시프트업"},
	}

	for _, company := range companies {
		if strings.Contains(
			textLower,
			strings.ToLower(company.Search),
		) {
			return company.Name
		}
	}

	return ""
}

func detectKeywords(
	text string,
) []string {

	var result []string

	groups := [][]string{
		alertKeywords,
		jobKeywords,
		industryKeywords,
	}

	used := make(map[string]bool)

	for _, group := range groups {
		for _, keyword := range group {

			if len(result) >= 6 {
				return result
			}

			if used[keyword] {
				continue
			}

			if strings.Contains(
				strings.ToLower(text),
				strings.ToLower(keyword),
			) {

				used[keyword] = true

				result = append(
					result,
					keyword,
				)
			}
		}
	}

	return result
}

func isSeen(id string) bool {
	seenMu.Lock()
	defer seenMu.Unlock()

	return seen[id]
}

func markSeen(id string) {
	seenMu.Lock()
	defer seenMu.Unlock()

	seen[id] = true
}

func publishedTime(
	item *gofeed.Item,
) time.Time {

	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}

	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}

	/*
		날짜 없는 RSS가 최신 기사로
		잘못 올라오는 것을 방지
	*/
	return time.Unix(0, 0)
}

func categoryColor(
	category string,
) int {

	switch category {

	case "채용 / 취업":

		// Discord green
		return 0x57F287

	case "업계 경보":

		// Discord red
		return 0xED4245

	case "산업 동향":

		// Discord blue
		return 0x5865F2

	default:

		return 0xFEE75C
	}
}

func cleanText(
	value string,
) string {

	value = html.UnescapeString(value)

	value = strings.ReplaceAll(
		value,
		"\n",
		" ",
	)

	value = strings.ReplaceAll(
		value,
		"\r",
		" ",
	)

	/*
		간단한 HTML Tag 제거
	*/
	for {
		start := strings.Index(
			value,
			"<",
		)

		end := strings.Index(
			value,
			">",
		)

		if start == -1 ||
			end == -1 ||
			end < start {
			break
		}

		value =
			value[:start] +
				value[end+1:]
	}

	value = strings.Join(
		strings.Fields(value),
		" ",
	)

	return strings.TrimSpace(value)
}

func truncate(
	value string,
	length int,
) string {

	runes := []rune(value)

	if len(runes) <= length {
		return value
	}

	return string(
		runes[:length],
	) + "..."
}
