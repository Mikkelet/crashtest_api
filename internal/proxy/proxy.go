package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"crashtest_api/internal/db"
)

const apiIDParam = "api-id"

type Handler struct {
	store   *db.Store
	logger  *slog.Logger
	proxy   *httputil.ReverseProxy
	matcher *patternCache
}

func New(store *db.Store, logger *slog.Logger) *Handler {
	h := &Handler{store: store, logger: logger, matcher: newPatternCache()}
	h.proxy = &httputil.ReverseProxy{
		Rewrite:      h.rewrite,
		ErrorHandler: h.proxyError,
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiID := r.URL.Query().Get(apiIDParam)
	if apiID == "" {
		http.Error(w, "missing required query parameter: api-id", http.StatusBadRequest)
		return
	}

	api, err := h.store.GetAPI(r.Context(), apiID)
	if errors.Is(err, db.ErrAPINotFound) {
		http.Error(w, "unknown api-id", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("lookup api", "error", err, "api_id", apiID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if h.tryIntercept(w, r, apiID) {
		return
	}

	target, err := url.Parse(api.BaseURL)
	if err != nil {
		h.logger.Error("invalid base url", "error", err, "api_id", apiID, "base_url", api.BaseURL)
		http.Error(w, "misconfigured api target", http.StatusBadGateway)
		return
	}

	ctx := context.WithValue(r.Context(), targetKey{}, target)
	h.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (h *Handler) tryIntercept(w http.ResponseWriter, r *http.Request, apiID string) bool {
	rules, err := h.store.ListEnabledInterceptRules(r.Context(), apiID)
	if err != nil {
		h.logger.Error("load intercept rules", "error", err, "api_id", apiID)
		return false
	}
	for _, rule := range rules {
		if !methodMatches(rule.Method, r.Method) {
			continue
		}
		re := h.matcher.get(rule.PathPattern)
		if re == nil || !re.MatchString(r.URL.Path) {
			continue
		}

		if rule.DelayMS > 0 {
			select {
			case <-time.After(time.Duration(rule.DelayMS) * time.Millisecond):
			case <-r.Context().Done():
				return true
			}
		}

		for k, v := range rule.ResponseHeaders {
			w.Header().Set(k, v)
		}
		status := rule.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if rule.ResponseBody != "" {
			_, _ = w.Write([]byte(rule.ResponseBody))
		}

		h.logger.Info("intercept",
			"api_id", apiID,
			"rule_id", rule.ID,
			"name", rule.Name,
			"status", status,
			"method", r.Method,
			"path", r.URL.Path,
		)
		return true
	}
	return false
}

func methodMatches(ruleMethod, requestMethod string) bool {
	if ruleMethod == "" || ruleMethod == "ANY" {
		return true
	}
	return strings.EqualFold(ruleMethod, requestMethod)
}

type targetKey struct{}

func (h *Handler) rewrite(pr *httputil.ProxyRequest) {
	target := pr.In.Context().Value(targetKey{}).(*url.URL)

	pr.Out.URL.Scheme = target.Scheme
	pr.Out.URL.Host = target.Host
	pr.Out.Host = target.Host

	pr.Out.URL.Path = singleJoiningSlash(target.Path, pr.In.URL.Path)
	if target.RawPath != "" || pr.In.URL.RawPath != "" {
		pr.Out.URL.RawPath = singleJoiningSlash(target.EscapedPath(), pr.In.URL.EscapedPath())
	}

	pr.Out.URL.RawQuery = stripAPIID(pr.In.URL.RawQuery)

	pr.SetXForwarded()
}

func (h *Handler) proxyError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("proxy error", "error", err, "url", r.URL.String())
	http.Error(w, "bad gateway", http.StatusBadGateway)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func stripAPIID(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	values.Del(apiIDParam)
	return values.Encode()
}

// patternCache compiles glob patterns to regex once and caches them.
// `*` matches any chars except `/`; `**` matches any chars including `/`.
type patternCache struct {
	mu sync.RWMutex
	m  map[string]*regexp.Regexp
}

func newPatternCache() *patternCache {
	return &patternCache{m: make(map[string]*regexp.Regexp)}
}

func (c *patternCache) get(pattern string) *regexp.Regexp {
	c.mu.RLock()
	re, ok := c.m[pattern]
	c.mu.RUnlock()
	if ok {
		return re
	}
	re = compileGlob(pattern)
	c.mu.Lock()
	c.m[pattern] = re
	c.mu.Unlock()
	return re
}

func compileGlob(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString("[^/]*")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(c)))
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
