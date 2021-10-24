package main

import (
	"backend/src/api"
	"backend/src/core"
	"github.com/codegangsta/martini"
	"metricWraper/metrics"
)

func main() {
	m := martini.Classic()
	m.Get("/api/self", core.LoginRequired, metrics.NewMetricWrappedHandler(api.GetSelf))
}
