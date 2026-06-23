package ia

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type IACredentials struct {
	AccessKey string
	SecretKey string
	Username  string
}

func LoadCredentials() (*IACredentials, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}

	iniPath := filepath.Join(home, ".config", "internetarchive", "ia.ini")
	data, err := os.ReadFile(iniPath)
	if err != nil {
		if access := os.Getenv("IA_ACCESS_KEY"); access != "" {
			return &IACredentials{
				AccessKey: access,
				SecretKey: os.Getenv("IA_SECRET_KEY"),
				Username:  os.Getenv("IA_USERNAME"),
			}, nil
		}
		return nil, fmt.Errorf("no IA credentials: %s not found (%v)", iniPath, err)
	}

	parsed := parseINI(string(data))

	section := parsed["s3"]
	if section == nil {
		section = parsed["default"]
	}

	if section == nil {
		return nil, fmt.Errorf("no [s3] or [default] section in %s", iniPath)
	}

	access := section["access"]
	if access == "" {
		access = section["s3_access_key"]
	}
	secret := section["secret"]
	if secret == "" {
		secret = section["s3_secret_key"]
	}

	if access == "" || secret == "" {
		return nil, fmt.Errorf("missing access/secret keys in [s3] section of %s", iniPath)
	}

	username := ""
	if g := parsed["general"]; g != nil {
		username = g["screenname"]
	}

	return &IACredentials{
		AccessKey: access,
		SecretKey: secret,
		Username:  username,
	}, nil
}

func (c *IACredentials) AuthHeader() string {
	return fmt.Sprintf("LOW %s:%s", c.AccessKey, c.SecretKey)
}

func (c *IACredentials) FavCollection() string {
	if c.Username != "" {
		return "fav-" + c.Username
	}
	return ""
}

func parseINI(content string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	var currentSection string

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(line[1 : len(line)-1])
			if result[currentSection] == nil {
				result[currentSection] = make(map[string]string)
			}
			continue
		}

		if currentSection == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		result[currentSection][key] = value
	}

	return result
}
