package service

import "time"

var weekdayTopics = map[time.Weekday]string{
	time.Monday:    "Education",
	time.Tuesday:   "Environment",
	time.Wednesday: "Technology",
	time.Thursday:  "Work/Career",
	time.Friday:    "Society",
	time.Saturday:  "Health",
	time.Sunday:    "Mixed Review/Weak",
}

func TopicForDate(localNow time.Time) string {
	if topic, ok := weekdayTopics[localNow.Weekday()]; ok {
		return topic
	}
	return "Mixed Review/Weak"
}
