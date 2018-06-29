package longevity

import (
	"os"
	"strconv"
)

func getEnvInt(env string, defaultVal int) int {
	envVal := os.Getenv(env)
	val, err := strconv.Atoi(envVal)
	if err == nil {
		return val
	}
	return defaultVal
}
