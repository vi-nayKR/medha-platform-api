package domain

import "github.com/google/uuid"

type City struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	City      string    `json:"city"`
	State     string    `json:"state"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	IsActive  bool      `json:"isActive"`
	CreatedAt int64     `json:"createdAt"`
	UpdatedAt int64     `json:"updatedAt"`
}

type NearbyCityResult struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	City       string    `json:"city"`
	State      string    `json:"state"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	DistanceKm float64   `json:"distanceKm"`
}

