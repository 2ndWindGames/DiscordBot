# Game News Bots

하나의 프로세스에서 두 Discord 봇을 함께 실행합니다.

- 게임 업계 동향 봇: RSS를 주기적으로 확인해 채용, 구조조정, 산업 뉴스를 전송
- Google Play 게임 트렌드 봇: 한국 Google Play의 인기 무료·인기 유료·최고 매출·신규 인기 차트를 각각 전송하고 제작 인사이트를 요약

## 설정

실행 설정은 봇별 파일로 분리되어 있으며 Git에 포함되지 않습니다.

```text
config/industry.json
config/google_play.json
```

처음 구성할 때 example 파일을 복사한 뒤 각 봇이 사용할 Discord webhook URL들을 `discord_webhook_urls` 배열에 입력합니다. 하나만 사용해도 됩니다.

```powershell
Copy-Item config/industry.example.json config/industry.json
Copy-Item config/google_play.example.json config/google_play.json
```

Google Play 봇의 주요 설정:

- `run_at`: 매일 실행할 시각 (`HH:MM`)
- `timezone`: 스케줄 기준 시간대 (기본값 `Asia/Seoul`)
- `country`, `language`: Google Play 국가와 언어
- `top_count`: Discord에 표시할 게임 수
- `state_file`: 전일 순위 비교용 스냅샷 파일

## 실행

봇 하나만 실행:

```powershell
go run . -type=industry
go run . -type=google_play
```

빌드한 실행 파일도 같은 방식으로 실행할 수 있습니다.

```powershell
./game-news-bot.exe -type=industry
./game-news-bot.exe -type=google_play
```

백그라운드로 실행하면서 현재 터미널에서 로그를 계속 보려면 `-background`를 추가합니다. 시작 로그에 백그라운드 프로세스 PID가 표시됩니다.

```powershell
./game-news-bot.exe -type=industry -background
./game-news-bot.exe -type=google_play -background
```

현재 터미널을 닫으면 로그 출력도 더 이상 볼 수 없습니다. 장기간 운영하고 로그를 보존하려면 서비스 관리자 또는 파일 리다이렉션을 함께 사용하는 것이 좋습니다.

두 봇을 한 프로세스에서 함께 실행하려면 `all`을 사용합니다. `-type`을 생략해도 기본값은 `all`입니다.

```powershell
go run . -type=all
```

첫 Google Play 보고서에는 모든 게임이 `NEW`로 표시되고, 다음 날부터 `▲`, `▼`, `－`로 전일 대비 변동을 보여줍니다. Google Play 페이지 구조가 바뀌어 수집하지 못할 경우 오류만 기록하며 이전 스냅샷은 보존합니다.

차트 순위는 Google Play에서 매일 수집해 공개하는 AppBrain 순위를 사용합니다. Discord에는 Google Play 상세 페이지가 공개하는 다운로드 구간과 평점·평가 수를 표시합니다. Google의 정확한 집계 기간·매출액·순위 알고리즘은 공개되지 않았으므로 의사결정 참고 자료로만 사용해야 합니다.
