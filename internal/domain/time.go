package domain

import "time"

func BoundsForLocalDate(now time.Time, timezone string) (date string, start time.Time, end time.Time, loc *time.Location, err error) {
	loc, err = time.LoadLocation(timezone)
	if err != nil {
		return "", time.Time{}, time.Time{}, nil, err
	}
	localNow := now.In(loc)
	date = localNow.Format("2006-01-02")
	start = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	end = start.Add(24*time.Hour - time.Nanosecond)
	return date, start.UTC(), end.UTC(), loc, nil
}
