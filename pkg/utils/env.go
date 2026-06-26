package utils

import (
	"os"
	"strings"
)

const (
	PROD = "prod"
	TEST = "test"
	DEV  = "dev"
)

func GetEnv() string {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = DEV
	}
	env = strings.ToLower(env)
	switch env {
	case PROD, TEST, DEV:
		return env
	default:
		return DEV
	}
}
