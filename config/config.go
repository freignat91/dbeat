// Config is put into a different package to prevent cyclic imports in case
// it is needed in several locations

package config

import "time"

// Config dbeat config
type Config struct {
	Period             time.Duration `config:"period"`
	DockerURL          string        `config:"docker_url"`
	TLS                bool          `config:"tls"`
	CaPath             string        `config:"ca_path"`
	CertPath           string        `config:"cert_path"`
	KeyPath            string        `config:"key_path"`
	Logs               bool          `config:"logs"`
	LogsDateSavePeriod int           `config:"logs_position_save_period"`
	Net                bool          `config:"net"`
	Memory             bool          `config:"memory"`
	IO                 bool          `config:"io"`
	CPU                bool          `config:"cpu"`
}

//DefaultConfig dbeat default config
var DefaultConfig = Config{
	Period:             3 * time.Second,
	DockerURL:          "unix:///var/run/docker.sock",
	Logs:               true,
	LogsDateSavePeriod: 10,
	Net:                true,
	Memory:             true,
	IO:                 true,
	CPU:                true,
}
