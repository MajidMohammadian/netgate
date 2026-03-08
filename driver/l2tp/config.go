package l2tp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// VPNConfig holds a single L2TP/IPsec connection for the UI and JSON storage.
type VPNConfig struct {
	ServerName      string `json:"server_name"`
	ServerAddress   string `json:"server_address"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	UsePreSharedKey *bool  `json:"use_pre_shared_key,omitempty"`
	PreSharedKey    string `json:"pre_shared_key"`
}

const configFileName = "config.json"

func configPath() string {
	return filepath.Join(dataDir, "l2tp", configFileName)
}

// LoadConfigs reads the driver config file as a JSON array; returns nil slice if file is missing or empty.
func LoadConfigs() ([]VPNConfig, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []VPNConfig
	if len(data) == 0 {
		return list, nil
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SaveConfigs writes the config list to the driver config file as an array.
func SaveConfigs(list []VPNConfig) error {
	if list == nil {
		list = []VPNConfig{}
	}
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
