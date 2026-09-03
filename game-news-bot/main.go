package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"sync"
)

const backgroundChildEnv = "GAME_NEWS_BOT_BACKGROUND_CHILD"

func main() {
	botType := flag.String("type", "all", "bot to run: industry, google_play, or all")
	background := flag.Bool("background", false, "run in background while keeping logs attached to this terminal")
	flag.Parse()

	if *background && os.Getenv(backgroundChildEnv) != "1" {
		startInBackground()
		return
	}

	log.Printf("process started: pid=%d type=%s background=%t", os.Getpid(), *botType, *background)

	switch *botType {
	case "industry":
		runIndustryBot(mustLoadIndustryConfig())

	case "google_play":
		runGooglePlayBot(mustLoadGooglePlayConfig())

	case "all":
		runAllBots()

	default:
		log.Fatalf("invalid -type %q: use industry, google_play, or all", *botType)
	}
}

func startInBackground() {
	command := exec.Command(os.Args[0], os.Args[1:]...)
	command.Env = append(os.Environ(), backgroundChildEnv+"=1")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		log.Fatalf("start background process: %v", err)
	}

	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil {
		log.Fatalf("release background process %d: %v", pid, err)
	}
	log.Printf("background process started: pid=%d", pid)
}

func runAllBots() {
	industryConfig := mustLoadIndustryConfig()
	googlePlayConfig := mustLoadGooglePlayConfig()

	var bots sync.WaitGroup
	bots.Add(2)
	go func() {
		defer bots.Done()
		runIndustryBot(industryConfig)
	}()
	go func() {
		defer bots.Done()
		runGooglePlayBot(googlePlayConfig)
	}()
	bots.Wait()
}

func mustLoadIndustryConfig() IndustryConfig {
	config, err := loadIndustryConfig("config/industry.json")
	if err != nil {
		log.Fatalf("industry config: %v", err)
	}
	return config
}

func mustLoadGooglePlayConfig() GooglePlayConfig {
	config, err := loadGooglePlayConfig("config/google_play.json")
	if err != nil {
		log.Fatalf("google play config: %v", err)
	}
	return config
}
