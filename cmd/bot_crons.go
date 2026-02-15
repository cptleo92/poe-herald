package main

import "github.com/robfig/cron/v3"

func (app *application) initializeCron() *cron.Cron {
	cr := cron.New(cron.WithSeconds())

	// cron.AddFunc("@daily", app.dailyDigest)
	// cron.AddFunc("@every 2s", app.pollCharacters)

	return cr
}

// dailyDigest messages the active channel with a daily digest
// func (app *application) dailyDigest() {
// }

// pollCharacters fetches regularly to keep chars updated and track deaths
// func (app *application) pollCharacters() {
// consider "rotating" fetches to not get rate limited
// mutex to prevent job overlap
// }
