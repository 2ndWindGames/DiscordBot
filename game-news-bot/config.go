package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type IndustryConfig struct {
	DiscordWebhookURLs   []string `json:"discord_webhook_urls"`
	DiscordWebhookURL    string   `json:"discord_webhook_url,omitempty"`
	CheckIntervalMinutes int      `json:"check_interval_minutes"`
	InitialArticleCount  int      `json:"initial_article_count"`
	MaxArticlesPerCheck  int      `json:"max_articles_per_check"`
}

func (c IndustryConfig) CheckInterval() time.Duration {
	return time.Duration(c.CheckIntervalMinutes) * time.Minute
}

type GooglePlayConfig struct {
	DiscordWebhookURLs []string `json:"discord_webhook_urls"`
	DiscordWebhookURL  string   `json:"discord_webhook_url,omitempty"`
	RunAt              string   `json:"run_at"`
	Timezone           string   `json:"timezone"`
	Country            string   `json:"country"`
	Language           string   `json:"language"`
	TopCount           int      `json:"top_count"`
	StateFile          string   `json:"state_file"`
}

func loadIndustryConfig(path string) (IndustryConfig, error) {
	var c IndustryConfig
	if err := loadJSON(path, &c); err != nil {
		return c, err
	}
	if len(c.WebhookURLs()) == 0 {
		return c, fmt.Errorf("discord_webhook_urls must contain at least one URL")
	}
	if c.CheckIntervalMinutes <= 0 {
		c.CheckIntervalMinutes = 180
	}
	if c.InitialArticleCount <= 0 {
		c.InitialArticleCount = 5
	}
	if c.MaxArticlesPerCheck <= 0 {
		c.MaxArticlesPerCheck = 5
	}
	return c, nil
}

func loadGooglePlayConfig(path string) (GooglePlayConfig, error) {
	var c GooglePlayConfig
	if err := loadJSON(path, &c); err != nil {
		return c, err
	}
	if len(c.WebhookURLs()) == 0 {
		return c, fmt.Errorf("discord_webhook_urls must contain at least one URL")
	}
	if c.RunAt == "" {
		c.RunAt = "09:00"
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Seoul"
	}
	if c.Country == "" {
		c.Country = "KR"
	}
	if c.Language == "" {
		c.Language = "ko"
	}
	if c.TopCount <= 0 {
		c.TopCount = 10
	}
	if c.StateFile == "" {
		c.StateFile = "data/google_play_previous.json"
	}
	return c, nil
}

func (c IndustryConfig) WebhookURLs() []string {
	return configuredWebhookURLs(c.DiscordWebhookURLs, c.DiscordWebhookURL)
}

func (c GooglePlayConfig) WebhookURLs() []string {
	return configuredWebhookURLs(c.DiscordWebhookURLs, c.DiscordWebhookURL)
}

func configuredWebhookURLs(urls []string, legacyURL string) []string {
	result := make([]string, 0, len(urls)+1)
	seen := make(map[string]bool)

	for _, webhookURL := range append(urls, legacyURL) {
		webhookURL = strings.TrimSpace(webhookURL)
		if webhookURL == "" || seen[webhookURL] {
			continue
		}
		seen[webhookURL] = true
		result = append(result, webhookURL)
	}
	return result
}

func loadJSON(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
