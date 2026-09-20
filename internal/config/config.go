package config

import (
	"log"

	"github.com/spf13/viper"
)

var (
	Port           string
	SharedPath     string
	MaxUploadSize  int64
	MaxPreviewSize int64
)

func init() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	viper.SetDefault("server.port", "8080")
	viper.SetDefault("storage.shared_path", "./shared")
	viper.SetDefault("storage.max_upload_size", 100*1024*1024)
	viper.SetDefault("storage.max_preview_size", 2*1024*1024)

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("config.yaml not found, using default configuration")
	}

	Port = viper.GetString("server.port")
	SharedPath = viper.GetString("storage.shared_path")
	MaxUploadSize = viper.GetInt64("storage.max_upload_size")
	MaxPreviewSize = viper.GetInt64("storage.max_preview_size")
}
