package domain

import "errors"

var (
	ErrCityNotFound  = errors.New("city not found")
	ErrCityNotActive = errors.New("city is not active")
)
