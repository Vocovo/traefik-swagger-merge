// Package traefik_swagger_merge is a middleware plugin that serves inline content from a configuration.
// Paths are matched by patterns that are defined in the configuration.
package traefik_swagger_merge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/Vocovo/traefik-swagger-merge/docs"
	"gopkg.in/yaml.v3"
)

// Config is the plugin configuration.
type Config struct {
	Path string     `json:"path"`
	Docs []*DocPath `json:"docs"`
}

type DocType int

const (
	DOC_TYPE_YAML DocType = iota
	DOC_TYPE_JSON
)

// DocPath is a path configuration.
type DocPath struct {
	// Path is the exact path to match.
	Path string `json:"path"`
	// PathRegex is a regular expression to match.
	PathRegex string `json:"pathRegex"`
	// Content is a go template of content to serve.
	Content string `json:"content"`
	// JSONData is a map of JSON data to return.
	JSONData map[string]any `json:"jsonData"`
	// Indent is the number of spaces to indent the JSON response.
	Indent int `json:"indent"`
	// Status is the HTTP status code to return.
	Status int `json:"status"`

	pathRegex *regexp.Regexp
	template  *template.Template
	jsonData  []byte
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Docs: make([]*DocPath, 0, 10),
	}
}

// SwaggerMerge is a plugin that merge multiply swagger docs into unified
type SwaggerMerge struct {
	next          http.Handler
	path          string
	pathRegexp    *regexp.Regexp
	refs          []DocPath
	name          string
	staticContent []byte
}

// New creates a new StaticResponse plugin.
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	jsonConfig, _ := json.Marshal(config)
	log.Default().Printf("⭕swagger-merge configuration: %v", string(jsonConfig))

	if len(config.Docs) == 0 {
		return nil, fmt.Errorf("⭕docs cannot be empty")
	}
	refs := make([]DocPath, len(config.Docs))
	for i, docPath := range config.Docs {
		ref := docPath
		// if err := ref.compile(); err != nil {
		// 	return nil, fmt.Errorf("invalid path configuration %s: %w", docPath.Path, err)
		// }
		refs[i] = *ref
	}
	pathRegexp, err := regexp.Compile(config.Path)
	if err != nil {
		log.Default().Printf("⭕path is not regexp %v", err)
	}

	return &SwaggerMerge{
		path:          config.Path,
		pathRegexp:    pathRegexp,
		refs:          refs,
		next:          next,
		name:          name,
		staticContent: docs.IndexHtml,
	}, nil
}

func (swaggerMerger *SwaggerMerge) GetMergedSwaggerDoc(docType DocType) (string, error) {
	// log.Default().Printf("⭕refs are %v", swaggerMerger.refs)
	result := make(map[any]any, 0)
	for _, ref := range swaggerMerger.refs {
		// Get the data
		resp, err := http.Get(ref.Path)
		if err != nil {
			log.Default().Printf("💍 error get an document by path %v (%v)", ref.Path, err)
			continue
		}
		defer resp.Body.Close()

		buf := bytes.NewBufferString("")
		// Writer the body to file
		_, err = io.Copy(buf, resp.Body)
		if err != nil {
			log.Default().Printf("💍 error get body issue: %v", err)
			continue
		}
		if strings.HasSuffix(ref.Path, ".yml") || strings.HasSuffix(ref.Path, ".yaml") {
			var swagger map[any]any
			err = yaml.Unmarshal(buf.Bytes(), &swagger)
			if err != nil {
				log.Default().Printf("💍 wrong yaml document format issue: %v", err)
				continue
			}
			swaggerMerger.deepRing(result, swagger)
			continue
		}
		if strings.HasSuffix(ref.Path, ".json") {
			var swagger map[any]any
			err = json.Unmarshal(buf.Bytes(), &swagger)
			if err != nil {
				log.Default().Printf("💍 wrong json document format issue: %v", err)
				continue
			}
			swaggerMerger.deepRing(result, swagger)
			continue
		}
	}

	if docType == DOC_TYPE_YAML {
		// Корректируем ссылки
		for key, value := range result {
			result[key] = swaggerMerger.referencesCorrection(key, value)
		}

		mergedDoc, err := yaml.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(mergedDoc), nil
	}
	if docType == DOC_TYPE_JSON {
		mergedDoc, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(mergedDoc), nil
	}
	return "", fmt.Errorf("unknown document type %v", docType)
}

func (swaggerMerger *SwaggerMerge) appendIfMissing(slice []any, newElement any) []any {
	for _, element := range slice {
		if element == newElement {
			return slice
		}
	}
	return append(slice, newElement)
}

// deepRing рекурсивно объединяет два YAML/JSON-объекта
func (swaggerMerger *SwaggerMerge) deepRing(dst, src map[any]any) {
	for key, srcVal := range src {
		// Если ключ уже есть в dst
		if dstVal, exists := dst[key]; exists {
			// log.Default().Printf("🔥 dstVal for %v is %T", key, dstVal)
			// log.Default().Printf("🔥 srcVal for %v is %T", key, srcVal)
			// Если оба значения — map, рекурсивно объединяем
			if dstMap, ok := dstVal.(map[any]any); ok {
				if srcMap, ok := srcVal.(map[any]any); ok {
					swaggerMerger.deepRing(dstMap, srcMap)
					dst[key] = dstMap
					continue
				}
			}
			// Если оба значения — slice, объединяем оставляя уникальные
			if dstSlice, ok := dstVal.([]any); ok {
				if srcSlice, ok := srcVal.([]any); ok {
					slicesUnion := append([]any{}, dstSlice...)
					for _, element := range srcSlice {
						slicesUnion = swaggerMerger.appendIfMissing(slicesUnion, element)
					}
					dst[key] = slicesUnion
					continue
				}
			}
		}
		// Иначе просто перезаписываем
		dst[key] = srcVal
	}
}

func (swaggerMerger *SwaggerMerge) referencesCorrection(key any, value any) any {
	// Map case
	if srcMap, ok := value.(map[any]any); ok {
		for childKey, childValue := range srcMap {
			srcMap[fmt.Sprintf("%v", childKey)] = swaggerMerger.referencesCorrection(childKey, childValue)
		}
	}
	// Slice case
	if srcSlice, ok := value.([]any); ok {
		for _, sliceElement := range srcSlice {
			if srcMap, ok := sliceElement.(map[any]any); ok {
				for childKey, childValue := range srcMap {
					srcMap[fmt.Sprintf("%v", childKey)] = swaggerMerger.referencesCorrection(childKey, childValue)
				}
			}
		}
	}

	// keys trigger
	if key == "$ref" {
		return fmt.Sprintf("'%v'", value)
	}

	if description, ok := value.(string); key == "description" && ok && strings.IndexAny(description, "':#") > -1 {
		return fmt.Sprintf("'%v'", strings.ReplaceAll(fmt.Sprint(value), "'", "`"))
	}

	return value
}

// ServeHTTP implements the http.Handler interface.
func (swaggerMerger *SwaggerMerge) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	path := swaggerMerger.path

	// log.Default().Printf("⭕request.path is %v, path is %v", req.URL.Path, path)

	if path != "" && (path == req.URL.Path) {
		if len(swaggerMerger.staticContent) > 0 {
			rw.Header().Set("Content-Type", "text/html")
			fmt.Fprint(rw, string(swaggerMerger.staticContent))
		}
		return
	}
	if path != "" && (strings.HasSuffix(req.URL.Path, ".yaml") || strings.HasSuffix(req.URL.Path, ".yml")) {
		rw.Header().Set("Content-Type", "application/yaml")
		mergedSwaggerDocument, err := swaggerMerger.GetMergedSwaggerDoc(DOC_TYPE_YAML)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
		fmt.Fprint(rw, mergedSwaggerDocument)
		return
	}
	if path != "" && (strings.HasSuffix(req.URL.Path, ".json")) {
		rw.Header().Set("Content-Type", "application/json")
		mergedSwaggerDocument, err := swaggerMerger.GetMergedSwaggerDoc(DOC_TYPE_JSON)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
		fmt.Fprint(rw, mergedSwaggerDocument)
		return
	}
	swaggerMerger.next.ServeHTTP(rw, req)
}
