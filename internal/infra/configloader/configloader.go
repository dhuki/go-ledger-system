package configloader

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type confValue string

var appConfig map[string]confValue

var defaultConfig = map[string]string{
	"app_name":            "go-ledger-system",
	"default_config_file": ".env,config/.env",
	"rest_port":           "8080",
	"graceful_timeout":    "30s",

	"postgres_host":                       "localhost",
	"postgres_port":                       "5432",
	"postgres_dbname":                     "ledger",
	"postgres_username":                   "ledger",
	"postgres_password":                   "",
	"postgres_ssl_mode":                   "disable",
	"postgres_migration_dir":              "internal/infra/database/repository/domain/migration",
	"postgres_max_connection":             "10",
	"postgres_max_idle_connection":        "5",
	"postgres_max_duration_idle_conn":     "5m",
	"postgres_max_duration_lifetime_conn": "1h",

	"redis_host":                     "localhost",
	"redis_port":                     "6379",
	"redis_password":                 "",
	"redis_db":                       "0",
	"redis_transfer_idempotency_ttl": "1h",
}

func LoadConfig() {
	fileList := defaultConfig["default_config_file"]
	if envVal := os.Getenv("DEFAULT_CONFIG_FILE"); envVal != "" {
		fileList = envVal
	}

	godotenv.Overload(strings.Split(fileList, ",")...)

	appConfig = make(map[string]confValue, 0)
	for key, value := range defaultConfig {
		appConfig[key] = confValue(value)
		if envValue := os.Getenv(strings.ToUpper(key)); envValue != "" {
			appConfig[key] = confValue(envValue)
		}
	}
}

func GetConfig(key string) (val confValue) {
	if v, ok := appConfig[strings.ToLower(key)]; ok {
		val = v
	}
	return
}

func (c confValue) String() string {
	return string(c)
}

func (c confValue) Int() int64 {
	if val, err := strconv.ParseInt(string(c), 10, 64); err == nil {
		return val
	}
	return 0
}

func (c confValue) Bool() bool {
	if val, err := strconv.ParseBool(string(c)); err == nil {
		return val
	}
	return false
}

func (c confValue) GetDuration() time.Duration {
	if val, err := time.ParseDuration(fmt.Sprintf("%s", c)); err == nil {
		return val
	}
	return 0
}
