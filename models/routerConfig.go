package models

type Header struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type RequestTransformerConfig struct {
	Replace struct {
		Headers []Header `yaml:"headers"`
	} `yaml:"replace"`
	Add struct {
		Headers []Header `yaml:"headers"`
	} `yaml:"add"`
}

type Plugin struct {
	Enabled bool        `yaml:"enabled"`
	Type    string      `yaml:"type"`
	Config  interface{} `yaml:"config"`
}

// Unmarshal plugin config to respective type
func (plugin *Plugin) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var t struct {
		Enabled bool   `yaml:"enabled"`
		Type    string `yaml:"type"`
	}

	if err := unmarshal(&t); err != nil {
		panic(err)
	}

	plugin.Enabled = t.Enabled
	plugin.Type = t.Type

	switch t.Type {
	case "request-transformer":
		var c struct {
			Config RequestTransformerConfig `yaml:"config"`
		}
		if err := unmarshal(&c); err != nil {
			panic(err)
		}
		plugin.Config = c.Config
	}
	return nil
}

type Catch struct {
	Host    string   `yaml:"host"`
	Headers []Header `yaml:"headers"`
}

type Dest struct {
	Host string `yaml:"host"`
	Port uint64 `yaml:"port"`
}

type Route struct {
	Name        string   `yaml:"name"`
	CatchConfig Catch    `yaml:"catch"`
	DestConfig  Dest     `yaml:"dest"`
	Plugins     []Plugin `yaml:"plugins"`
}

type Endpoint struct {
	Name   string  `yaml:"name"`
	Method string  `yaml:"method"`
	Path   string  `yaml:"path"`
	Routes []Route `yaml:"routes"`
}

type EndpointsConfig struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}
