package env

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func AttemptReadLocalEnvironment(path string) error {
	if os.Getenv("APP_ENV") != "local" {
		return nil
	}
	return godotenv.Load(path)
}

func MustGet(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatal("Could not get value for environment variable ", key)
	}
	return v
}

func CanGet(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func MustGetInt(key string) int {
	v, err := strconv.Atoi(MustGet(key))
	if err != nil {
		log.Fatalf("Could not convert environment variable %s to int, err: %s ", key, err)
	}
	return v
}

func CanGetInt(key string, defaultValue int) int {
	v, err := strconv.Atoi(CanGet(key))
	if err != nil {
		return defaultValue
	}
	return v
}

func CanGetFloat64(key string, defaultValue float64) float64 {
	v, err := strconv.ParseFloat(CanGet(key), 32)
	if err != nil {
		return defaultValue
	}
	return v
}

func MustGetBool(key string) bool {
	v, err := strconv.ParseBool(MustGet(key))
	if err != nil {
		log.Fatalf("Could not convert environment variable %s to bool, err: %s ", key, err)
	}
	return v
}

func CanGetBool(key string) bool {
	v, err := strconv.ParseBool(CanGet(key))
	if err != nil {
		return false
	}
	return v
}
