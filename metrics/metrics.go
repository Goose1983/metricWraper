package metrics

import (
	"fmt"
	"github.com/codegangsta/martini"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/shirou/gopsutil/host"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"urms/api"
	"urms/application"
	"urms/models"
)

var metricLabels = []string{"statusCode", "method", "pathPattern", "params", "instance", "user"}
var counterVec *prometheus.CounterVec
var endpointsRegexps map[string]*regexp.Regexp

func InitMetrics(app application.App, m *martini.ClassicMartini) {
	routes := m.All()

	endpointsRegexps = make(map[string]*regexp.Regexp)
	for _, route := range routes {
		pattern := route.Pattern()

		// не отслеживаем паттерны с **, это редиректы для необслуживаемых путей и пингеры
		reAsterisk := regexp.MustCompile(`\*+`)
		if reAsterisk.Match([]byte(pattern)) {
			continue
		}

		reParameter := regexp.MustCompile(`/:\w+`)
		pathMask := reParameter.ReplaceAll([]byte(pattern), []byte(`/(.+)`)) // заменяем параметры из паттерна на непустые строки
		pathMask = []byte("^" + string(pathMask) + "$")
		rePathMask := regexp.MustCompile(string(pathMask))

		if _, ok := endpointsRegexps[pattern]; !ok {
			endpointsRegexps[pattern] = rePathMask
		}
	}

	counterVec = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "endpoint_usage_counter",
			Help: "Суммарное количество запросов",
		},
		metricLabels,
	)

	hostStat, _ := host.Info()
	opsQueued := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "build_info",
		Help: "Общая информация",
		ConstLabels: map[string]string{
			"version":       app.Config.Server.Version,
			"environment":   app.Config.Server.Environment,
			"hostname":      hostStat.Hostname,
			"container_tag": "",
		},
	})
	prometheus.MustRegister(opsQueued)
	opsQueued.Inc() //чтобы в метрике была единичка
}

type metricResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (mrw *metricResponseWriter) WriteHeader(code int) {
	mrw.statusCode = code
	mrw.ResponseWriter.WriteHeader(code)
}

func NewMetricResponseWriter(w http.ResponseWriter) *metricResponseWriter {
	// Внутренний handler может и не заполнить статус, исходя из того, что это сделает
	// martini. Поэтому по умолчанию делаем 200
	return &metricResponseWriter{w, http.StatusOK}
}

func NewMetricWrappedHandler(handlerToWrapWithMetric api.AutoHandler) api.AutoHandler {
	wrappedHandler := func(app application.App, user models.User, w http.ResponseWriter, r *http.Request, params martini.Params) {

		mrw := NewMetricResponseWriter(w)
		handlerToWrapWithMetric(app, user, mrw, r, params)

		endpoint := r.URL.Path
		for pathPattern := range endpointsRegexps {
			matches := endpointsRegexps[pathPattern].FindStringSubmatch(endpoint)
			if matches != nil {
				statusCode := strconv.Itoa(mrw.statusCode)
				// labels = {"statusCode", "method", "pathPattern", "params", "instance", "user"}
				paramLabel := ""
				if params != nil {
					var paramRows []string
					for paramKey, paramVal := range params {
						paramRows = append(paramRows, fmt.Sprint("param_", paramKey, "=\"", paramVal, "\""))
					}
					paramLabel = strings.Join(paramRows, " ")
				}
				counterVec.WithLabelValues(statusCode, r.Method, pathPattern, paramLabel, r.Host, user.GetUserName()).Inc()
			}
		}
	}

	return wrappedHandler
}
