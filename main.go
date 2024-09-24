package main

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"

	"github.com/davecgh/go-spew/spew"
	"github.com/gin-gonic/gin"
	"github.com/protofire/proteus-shield/go-proxy-service-test/models"
	"gopkg.in/yaml.v3"
)

func isRequestMatchesRouteByHeaders(ctx *gin.Context, route models.Route, c chan bool) {
	matches := true
	for _, headerCatchConfig := range route.CatchConfig.Headers {
		re := regexp.MustCompile(headerCatchConfig.Value)
		currentHeaderValue := ctx.Request.Header.Get(headerCatchConfig.Name)
		if !re.Match([]byte(currentHeaderValue)) {
			matches = false
			break
		}
	}
	c <- matches
}

func isRequestMatchesRouteByQueryParams(ctx *gin.Context, route models.Route, c chan bool) {
	matches := true
	for _, paramCatchConfig := range route.CatchConfig.Params {
		re := regexp.MustCompile(paramCatchConfig.Value)
		currentParamValue := ctx.Query(paramCatchConfig.Name)
		if !re.Match([]byte(currentParamValue)) {
			matches = false
			break
		}
	}
	c <- matches
}

// Find the first route that matches the request
func findRoute(ctx *gin.Context, endpoint models.Endpoint) (*models.Route, error) {
	routeIndex := slices.IndexFunc(endpoint.Routes, func(route models.Route) bool {
		matchesRouteByHeadersChannel := make(chan bool)
		go isRequestMatchesRouteByHeaders(ctx, route, matchesRouteByHeadersChannel)

		matchesRouteByQueryParamsChannel := make(chan bool)
		go isRequestMatchesRouteByQueryParams(ctx, route, matchesRouteByQueryParamsChannel)

		return route.CatchConfig.Host == ctx.Request.Host && <-matchesRouteByHeadersChannel && <-matchesRouteByQueryParamsChannel
	})

	if routeIndex == -1 {
		return nil, errors.New("route not found")
	}

	return &endpoint.Routes[routeIndex], nil
}

func replaceRequestHeaderIfExists(ctx *gin.Context, header models.Header) {
	if currentValue := ctx.Request.Header.Get(header.Name); currentValue != "" {
		ctx.Request.Header.Set(header.Name, header.Value)
	}
}

func addRequestHeaderIfNotExists(ctx *gin.Context, header models.Header) {
	if currentValue := ctx.Request.Header.Get(header.Name); currentValue == "" {
		ctx.Request.Header.Add(header.Name, header.Value)
	}
}

// Put plugin logic in similar functions
func handleRequestTransformerPlugin(ctx *gin.Context, config models.RequestTransformerConfig) error {
	for _, header := range config.Replace.Headers {
		go replaceRequestHeaderIfExists(ctx, header)
	}

	for _, header := range config.Add.Headers {
		go addRequestHeaderIfNotExists(ctx, header)
	}

	return nil
}

func handleHttp(ctx *gin.Context, endpoint models.Endpoint) {
	route, err := findRoute(ctx, endpoint)
	if err != nil {
		ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}

	remote, err := url.Parse("http://" + route.DestConfig.Host + ":" + strconv.FormatUint(route.DestConfig.Port, 10))
	if err != nil {
		ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
		return
	}

	// Put plugins chain here. The plugins are executed in order they appear in config
	for _, plugin := range route.Plugins {
		// We can disable plugins in config without removing them.
		// It's useful for testing and stuff
		if !plugin.Enabled {
			continue
		}

		switch plugin.Type {
		case "request-transformer":
			if err := handleRequestTransformerPlugin(ctx, plugin.Config.(models.RequestTransformerConfig)); err != nil {
				ctx.AbortWithStatusJSON(500, gin.H{"error": err.Error()})
				return
			}
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.Director = func(r *http.Request) {
		r.Header = ctx.Request.Header
		r.Host = remote.Host
		r.URL.Scheme = remote.Scheme
		r.URL.Host = remote.Host
		r.URL.Path = endpoint.Path + ctx.Param("proxyPath")
	}

	proxy.ServeHTTP(ctx.Writer, ctx.Request)
}

// This is essentially a handler chooser
func proxy(endpoint models.Endpoint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		handleHttp(ctx, endpoint)
	}
}

func loadRouterConfig(path string, config interface{}) error {
	file, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	if err := yaml.Unmarshal(file, config); err != nil {
		panic(err)
	}

	return nil
}

func main() {
	var routerConfig models.RouterConfig
	loadRouterConfig("./config.yaml", &routerConfig)
	spew.Dump((routerConfig))

	router := gin.Default()
	for _, endpoint := range routerConfig.Endpoints {
		switch endpoint.Method {
		case "POST":
			router.POST(endpoint.Path, proxy(endpoint))
		case "GET":
			router.GET(endpoint.Path, proxy(endpoint))
		}
	}
	router.Run()
}
