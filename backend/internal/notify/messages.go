// Package notify creates notifications (inbox + push queue) and delivers push
// messages through FCM.
package notify

import (
	"fmt"
	"math"
)

// Notification types (the "type" in the inbox and in the push data).
const (
	TypeBusStarted    = "bus_started"
	TypeApproaching   = "bus_approaching"
	TypeReached       = "bus_reached"
	TypeCrossed       = "bus_crossed"
	TypeDelayed       = "bus_delayed"
	TypeBreakdown     = "bus_breakdown"
	TypeSchoolReached = "school_reached"
	TypeTripCompleted = "trip_completed"
	TypeAnnouncement  = "announcement"
)

// Message is a title and body. Wording follows CLAUDE.md "Reference copy".
type Message struct{ Title, Body string }

func tripLabel(tripType string) string {
	if tripType == "evening_drop" {
		return "Evening Drop"
	}
	return "Morning Pickup"
}

func BusStarted(tripType, busNumber string) Message {
	if tripType == "evening_drop" {
		return Message{"Bus started", fmt.Sprintf("Bus Started from School (%s).", busNumber)}
	}
	return Message{"Bus started", fmt.Sprintf("The Morning Pickup bus (%s) has started its route.", busNumber)}
}

// Approaching includes the ETA when known: "Estimated arrival: 8 minutes."
func Approaching(etaSeconds *int) Message {
	body := "Bus is approaching your stop."
	if etaSeconds != nil {
		mins := int(math.Max(1, math.Round(float64(*etaSeconds)/60)))
		unit := "minutes"
		if mins == 1 {
			unit = "minute"
		}
		body += fmt.Sprintf(" Estimated arrival: %d %s.", mins, unit)
	}
	return Message{"Bus approaching", body}
}

func Reached() Message { return Message{"Bus at your stop", "Bus reached your stop."} }

func Crossed(missed bool) Message {
	if missed {
		return Message{"Bus passed your stop", "Bus has crossed your stop without stopping. Please contact the school."}
	}
	return Message{"Bus left your stop", "Bus has crossed your stop."}
}

func Delayed(minutes int, tripType string) Message {
	where := "pickup"
	if tripType == "evening_drop" {
		where = "drop"
	}
	return Message{"Bus delayed", fmt.Sprintf("Bus Delayed by %d Minutes. Please expect a delay in reaching your %s location.", minutes, where)}
}

func Breakdown() Message {
	return Message{"Important Bus Update", "Important Bus Update: Your school bus has experienced a breakdown. Please wait for further instructions from the school."}
}

func SchoolReached() Message {
	return Message{"Reached school", "The bus has reached school."}
}

func TripCompleted(tripType string) Message {
	return Message{"Trip completed", fmt.Sprintf("The %s trip is complete.", tripLabel(tripType))}
}
