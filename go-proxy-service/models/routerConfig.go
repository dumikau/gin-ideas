package models

import "github.com/creasty/defaults"

type Header struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type QueryParam struct {
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
	Disabled bool
	Type     string
	Config   interface{}
}

// Unmarshal plugin config to respective type
func (plugin *Plugin) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var t struct {
		Disabled bool   `yaml:"disabled"`
		Type     string `yaml:"type"`
	}

	if err := unmarshal(&t); err != nil {
		panic(err)
	}

	plugin.Disabled = t.Disabled
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
	Host    string       `yaml:"host"`
	Headers []Header     `yaml:"headers"`
	Params  []QueryParam `yaml:"params"`
}

type Dest struct {
	Host             string `yaml:"host"`
	Port             uint64 `yaml:"port"`
	RemovePathPrefix bool   `yaml:"remove_path_prefix"`
	Path             string `yaml:"path"`
	Method           string `yaml:"method"`
}

type Route struct {
	Name        string   `yaml:"name"`
	CatchConfig Catch    `yaml:"catch"`
	DestConfig  Dest     `yaml:"dest"`
	Plugins     []Plugin `yaml:"plugins"`
}

type Endpoint struct {
	Name     string  `yaml:"name"`
	Method   string  `yaml:"method"`
	Path     string  `yaml:"path"`
	PathMode string  `yaml:"path_mode" default:"Exact"`
	Routes   []Route `yaml:"routes"`
}

func (endpoint *Endpoint) UnmarshalYAML(unmarshal func(interface{}) error) error {
	defaults.Set(endpoint)

	type plain Endpoint
	if err := unmarshal((*plain)(endpoint)); err != nil {
		return err
	}

	return nil
}

type RouterConfig struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}
