package service

import "time"

var rotatingTopics = []string{
	"Education",
	"Environment",
	"Technology",
	"Work/Career",
	"Society",
	"Health",
	"Business",
	"Finance",
	"Communication",
	"Travel",
	"Science",
	"Media",
	"Culture",
	"Law/Government",
	"Psychology",
	"Relationships",
	"Daily Life",
	"Mixed Review/Weak",
}

func TopicForDate(localNow time.Time) string {
	if len(rotatingTopics) == 0 {
		return "Mixed Review/Weak"
	}
	civilMidnightUTC := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	dayIndex := int(civilMidnightUTC.Unix() / 86400)
	if dayIndex < 0 {
		dayIndex = -dayIndex
	}
	return rotatingTopics[dayIndex%len(rotatingTopics)]
}
