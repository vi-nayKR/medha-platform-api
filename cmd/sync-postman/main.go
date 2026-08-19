package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Swagger struct {
	Paths       map[string]map[string]Operation `json:"paths"`
	Definitions map[string]Schema               `json:"definitions"`
	Tags        []Tag                           `json:"tags"`
}

type Tag struct {
	Name string `json:"name"`
}

type Operation struct {
	Summary    string      `json:"summary"`
	Tags       []string    `json:"tags"`
	Parameters []Parameter `json:"parameters"`
}

type Parameter struct {
	Name    string      `json:"name"`
	In      string      `json:"in"`
	Default interface{} `json:"default"`
	Schema  *Schema     `json:"schema"`
}

type Schema struct {
	Ref        string            `json:"$ref"`
	Type       string            `json:"type"`
	Required   []string          `json:"required"`
	Properties map[string]Schema `json:"properties"`
	Items      *Schema           `json:"items"`
	Example    interface{}       `json:"example"`
	Enum       []interface{}     `json:"enum"`
}

// Postman collection formats
type PostmanCollection struct {
	Info     PostmanInfo   `json:"info"`
	Item     []PostmanItem `json:"item"`
	Auth     *PostmanAuth  `json:"auth,omitempty"`
	Variable []PostmanVar  `json:"variable,omitempty"`
}

type PostmanInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema"`
}

type PostmanItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Item        []PostmanItem   `json:"item,omitempty"`    // For folders
	Request     *PostmanRequest `json:"request,omitempty"` // For requests
}

type PostmanRequest struct {
	Method string          `json:"method"`
	Header []PostmanHeader `json:"header,omitempty"`
	Body   *PostmanBody    `json:"body,omitempty"`
	URL    PostmanURL      `json:"url"`
}

type PostmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PostmanBody struct {
	Mode string `json:"mode"`
	Raw  string `json:"raw,omitempty"`
}

type PostmanURL struct {
	Raw      string             `json:"raw"`
	Host     []string           `json:"host"`
	Path     []string           `json:"path"`
	Query    []PostmanQueryItem `json:"query,omitempty"`
	Variable []PostmanPathVar   `json:"variable,omitempty"`
}

type PostmanQueryItem struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type PostmanPathVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PostmanAuth struct {
	Type   string            `json:"type"`
	Bearer []PostmanAuthAttr `json:"bearer,omitempty"`
}

type PostmanAuthAttr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

type PostmanVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func main() {
	swaggerPath := "api/openapi/swagger.json"
	if _, err := os.Stat(swaggerPath); os.IsNotExist(err) {
		swaggerPath = "docs/swagger.json"
	}
	postmanDir := "api/postman"

	data, err := os.ReadFile(swaggerPath)
	if err != nil {
		fmt.Printf("Error reading swagger file: %v. Run 'make swagger' first.\n", err)
		os.Exit(1)
	}

	var swag Swagger
	if err := json.Unmarshal(data, &swag); err != nil {
		fmt.Printf("Error parsing swagger.json: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(postmanDir, 0755); err != nil {
		fmt.Printf("Error creating postman dir: %v\n", err)
		os.Exit(1)
	}

	// Create map to group items by folder/tag
	folderMap := make(map[string][]PostmanItem)
	reSafePathVar := regexp.MustCompile(`{(\w+)}`)

	pathNames := make([]string, 0, len(swag.Paths))
	for pathStr := range swag.Paths {
		pathNames = append(pathNames, pathStr)
	}
	sort.Strings(pathNames)

	for _, pathStr := range pathNames {
		methods := swag.Paths[pathStr]
		methodNames := make([]string, 0, len(methods))
		for method := range methods {
			methodNames = append(methodNames, method)
		}
		sort.Strings(methodNames)

		for _, method := range methodNames {
			op := methods[method]
			tagName := "General"
			if len(op.Tags) > 0 {
				tagName = op.Tags[0]
			}

			name := op.Summary
			if name == "" {
				name = fmt.Sprintf("%s %s", strings.ToUpper(method), pathStr)
			}

			// Format path: replace {param} with :param for Postman path variables
			postmanPathStr := reSafePathVar.ReplaceAllString(pathStr, ":$1")

			// Build path components (split by / and skip empty parts)
			trimmedPath := strings.Trim(postmanPathStr, "/")
			var pathComponents []string
			if trimmedPath != "" {
				pathComponents = strings.Split(trimmedPath, "/")
			}

			// Headers
			headers := []PostmanHeader{}
			var bodySchema *Schema
			var queryParams []Parameter
			var pathVars []string

			// Extract parameters
			for _, p := range op.Parameters {
				switch p.In {
				case "query":
					queryParams = append(queryParams, p)
				case "body":
					bodySchema = p.Schema
					headers = append(headers, PostmanHeader{
						Key:   "Content-Type",
						Value: "application/json",
					})
				case "path":
					pathVars = append(pathVars, p.Name)
				}
			}

			// Postman Request Body
			var body *PostmanBody
			if bodySchema != nil {
				sample := sampleForOperation(strings.ToLower(method), pathStr, bodySchema, swag.Definitions)
				sampleJSON, err := json.MarshalIndent(sample, "", "  ")
				if err == nil {
					body = &PostmanBody{
						Mode: "raw",
						Raw:  string(sampleJSON),
					}
				}
			}

			// Postman Request Query Items
			var postmanQuery []PostmanQueryItem
			for _, q := range queryParams {
				val := ""
				if q.Default != nil {
					val = fmt.Sprintf("%v", q.Default)
				}
				postmanQuery = append(postmanQuery, PostmanQueryItem{
					Key:   q.Name,
					Value: val,
				})
			}

			// Postman Path Variables
			var postmanPathVars []PostmanPathVar
			// Look for matching path variables in the path components
			matches := reSafePathVar.FindAllStringSubmatch(pathStr, -1)
			for _, match := range matches {
				if len(match) == 2 {
					varName := match[1]
					postmanPathVars = append(postmanPathVars, PostmanPathVar{
						Key:   varName,
						Value: sampleString(varName),
					})
				}
			}

			request := &PostmanRequest{
				Method: strings.ToUpper(method),
				Header: headers,
				Body:   body,
				URL: PostmanURL{
					Raw:      "{{base_url}}/" + strings.TrimPrefix(postmanPathStr, "/"),
					Host:     []string{"{{base_url}}"},
					Path:     pathComponents,
					Query:    postmanQuery,
					Variable: postmanPathVars,
				},
			}

			item := PostmanItem{
				Name:    name,
				Request: request,
			}

			folderMap[tagName] = append(folderMap[tagName], item)
		}
	}

	// Sort tags/folders
	var tagNames []string
	for tag := range folderMap {
		tagNames = append(tagNames, tag)
	}
	sort.Strings(tagNames)

	var items []PostmanItem
	for _, folderName := range tagNames {
		items = append(items, PostmanItem{
			Name: folderName,
			Item: folderMap[folderName],
		})
	}

	collection := PostmanCollection{
		Info: PostmanInfo{
			Name:   "Medha API",
			Schema: "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Item: items,
		Auth: &PostmanAuth{
			Type: "bearer",
			Bearer: []PostmanAuthAttr{
				{
					Key:   "token",
					Value: "{{bearer_token}}",
					Type:  "string",
				},
			},
		},
		Variable: []PostmanVar{
			{
				Key:   "base_url",
				Value: "http://localhost:8080",
				Type:  "string",
			},
		},
	}

	collectionJSON, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		fmt.Printf("Error marshalling collection: %v\n", err)
		os.Exit(1)
	}

	outputFilePath := filepath.Join(postmanDir, "medha_api_collection.json")
	if err := os.WriteFile(outputFilePath, collectionJSON, 0644); err != nil {
		fmt.Printf("Error writing postman collection file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated Postman Collection at %s\n", outputFilePath)
}

func sampleForOperation(method, path string, schema *Schema, definitions map[string]Schema) any {
	if sample, ok := explicitBodySample(method, path); ok {
		return sample
	}
	return sampleFromSchema(schema, definitions, "")
}

func explicitBodySample(method, path string) (any, bool) {
	key := method + " " + path
	samples := map[string]any{
		"post /api/v2/event": map[string]any{
			"ceremony_type":               "custom",
			"custom_ceremony_name":        "Gudli Pooja",
			"custom_ceremony_description": "A family ritual performed before the main ceremony. Include vidhi, samagri, gotra, regional custom, and preferred language notes here.",
			"event_date":                  "2026-06-15",
			"latitude":                    12.9716,
			"longitude":                   77.5946,
			"address":                     "12 MG Road, Bengaluru, Karnataka",
			"description":                 "Please arrive by 7 AM. The family will arrange flowers and fruits.",
		},
		"put /api/v2/event/{id}": map[string]any{
			"ceremony_type":               "custom",
			"custom_ceremony_name":        "Gudli Pooja",
			"custom_ceremony_description": "Updated ritual details, preferred vidhi, samagri list, and regional practice notes.",
			"event_date":                  "2026-06-16",
			"latitude":                    12.9716,
			"longitude":                   77.5946,
			"address":                     "Updated venue address",
			"description":                 "Updated logistics note for the pandit.",
		},
		"post /api/v2/admin/bookings": map[string]any{
			"yajman_id": "550e8400-e29b-41d4-a716-446655440000",
			"pandit_id": "550e8400-e29b-41d4-a716-446655440001",
			"event_id":  "550e8400-e29b-41d4-a716-446655440002",
			"status":    "created",
		},
		"put /api/v2/admin/bookings/{id}": map[string]any{
			"status": "active",
		},
		"post /api/v2/admin/ceremony-logos": map[string]any{
			"name":            "Gudli Pooja",
			"slug":            "gudli-pooja",
			"image_url":       "ceremonies/gudli-pooja.png",
			"image_url_no_bg": "ceremonies/gudli-pooja-nobg.png",
			"category":        "custom",
			"description":     "Ritual logo entry shown in the ceremony picker.",
			"is_active":       true,
			"display_order":   100,
		},
		"put /api/v2/admin/ceremony-logos/{id}": map[string]any{
			"name":            "Gudli Pooja",
			"slug":            "gudli-pooja",
			"image_url":       "ceremonies/gudli-pooja.png",
			"image_url_no_bg": "ceremonies/gudli-pooja-nobg.png",
			"category":        "custom",
			"description":     "Updated ceremony picker details.",
			"is_active":       true,
			"display_order":   100,
		},
		"post /api/v2/admin/conversations": map[string]any{
			"type":            "pandit_yajman",
			"title":           "Ceremony discussion",
			"participant_ids": []string{"550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"},
			"event_id":        "550e8400-e29b-41d4-a716-446655440002",
			"match_id":        "550e8400-e29b-41d4-a716-446655440003",
		},
		"put /api/v2/admin/conversations/{id}": map[string]any{
			"title":     "Updated ceremony discussion",
			"is_active": true,
		},
		"post /api/v2/admin/credentials": map[string]any{
			"username": "admin@example.com",
			"password": "replace-with-strong-password",
			"role":     "admin",
			"active":   true,
		},
		"put /api/v2/admin/credentials/{id}": map[string]any{
			"password": "replace-with-strong-password",
			"role":     "admin",
			"active":   true,
		},
		"post /api/v2/admin/events": map[string]any{
			"yajman_id":                   "550e8400-e29b-41d4-a716-446655440000",
			"ceremony_type":               "custom",
			"custom_ceremony_name":        "Gudli Pooja",
			"custom_ceremony_description": "Admin-created custom ceremony details.",
			"event_date":                  1783900800,
			"latitude":                    12.9716,
			"longitude":                   77.5946,
			"address":                     "12 MG Road, Bengaluru",
			"description":                 "Admin logistics note.",
			"status":                      "Created",
		},
		"put /api/v2/admin/events/{id}": map[string]any{
			"yajman_id":                   "550e8400-e29b-41d4-a716-446655440000",
			"ceremony_type":               "custom",
			"custom_ceremony_name":        "Gudli Pooja",
			"custom_ceremony_description": "Updated admin ceremony details.",
			"event_date":                  1783987200,
			"latitude":                    12.9716,
			"longitude":                   77.5946,
			"address":                     "Updated venue address",
			"description":                 "Updated admin logistics note.",
			"status":                      "Active",
		},
		"post /api/v2/admin/festivals": map[string]any{
			"festival":    "Ugadi",
			"date":        1783900800,
			"description": "Festival description shown in panchanga.",
		},
		"put /api/v2/admin/festivals/{id}": map[string]any{
			"festival":    "Ugadi",
			"date":        1783987200,
			"description": "Updated festival description.",
		},
		"post /api/v2/admin/notifications": map[string]any{
			"user_id": "550e8400-e29b-41d4-a716-446655440000",
			"type":    "system",
			"title":   "New update",
			"body":    "Your ceremony details were updated.",
			"data":    map[string]string{"event_id": "550e8400-e29b-41d4-a716-446655440002"},
		},
		"put /api/v2/admin/notifications/{id}": map[string]any{
			"read": true,
		},
		"post /api/v2/admin/panchanga": map[string]any{
			"date":             1783900800,
			"samvatsara":       "Vishvavasu",
			"ayana":            "Uttarayana",
			"rutu":             "Grishma",
			"masa":             "Jyeshtha",
			"paksha":           "Shukla",
			"tithi":            "Pratipada",
			"nakshatra":        "Rohini",
			"yoga":             "Siddhi",
			"karana":           "Bava",
			"vasara":           "Monday",
			"festivals_events": "Gudli Pooja",
			"sunrise":          "06:01",
			"sunset":           "18:45",
			"rahukala":         "07:30-09:00",
		},
		"put /api/v2/admin/panchanga/{id}": map[string]any{
			"festivals_events": "Updated festival/event notes",
			"sunrise":          "06:02",
			"sunset":           "18:46",
		},
		"post /api/v2/admin/posts": map[string]any{
			"author_id":  "550e8400-e29b-41d4-a716-446655440000",
			"content":    "Ceremony preparation tip.",
			"image_urls": []string{"posts/example.jpg"},
			"tags":       []string{"ceremony", "tips"},
		},
		"put /api/v2/admin/posts/{id}": map[string]any{
			"content":    "Updated ceremony preparation tip.",
			"image_urls": []string{"posts/example.jpg"},
			"tags":       []string{"ceremony", "tips"},
		},
		"post /api/v2/admin/users": map[string]any{
			"first_name": "Synthetic",
			"last_name":  "User",
			"role":       "yajman",
			"phone":      "+919999999999",
			"email":      "user@example.com",
			"username":   "synthetic-user",
		},
		"put /api/v2/admin/users/{id}": map[string]any{
			"first_name": "Synthetic",
			"last_name":  "User",
			"role":       "yajman",
			"email":      "user@example.com",
			"username":   "synthetic-user",
		},
	}
	sample, ok := samples[key]
	return sample, ok
}

func sampleFromSchema(schema *Schema, definitions map[string]Schema, propName string) any {
	if schema == nil {
		return map[string]any{}
	}
	if schema.Ref != "" {
		name := strings.TrimPrefix(schema.Ref, "#/definitions/")
		if def, ok := definitions[name]; ok {
			return sampleFromSchema(&def, definitions, propName)
		}
		return map[string]any{}
	}
	if schema.Example != nil {
		return schema.Example
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}

	switch schema.Type {
	case "object":
		result := make(map[string]any, len(schema.Properties))
		for name, prop := range schema.Properties {
			result[name] = sampleFromSchema(&prop, definitions, name)
		}
		return result
	case "array":
		return []any{sampleFromSchema(schema.Items, definitions, propName)}
	case "integer":
		return sampleNumber(propName, true)
	case "number":
		return sampleNumber(propName, false)
	case "boolean":
		return true
	case "string":
		return sampleString(propName)
	default:
		if len(schema.Properties) > 0 {
			result := make(map[string]any, len(schema.Properties))
			for name, prop := range schema.Properties {
				result[name] = sampleFromSchema(&prop, definitions, name)
			}
			return result
		}
		return map[string]any{}
	}
}

func sampleString(name string) string {
	switch name {
	case "phone":
		return "+919999999999"
	case "id", "event_id", "match_id", "pandit_id", "conversation_id":
		return "550e8400-e29b-41d4-a716-446655440000"
	case "id_token":
		return "eyJhbGciOiJSUzI1NiJ9.example"
	case "refresh_token", "token":
		return "replace-with-token"
	case "type":
		return "pandit_yajman"
	case "content_type":
		return "text"
	case "content":
		return "Namaste, I can help with this ceremony."
	case "title":
		return "Conversation title"
	case "message":
		return "I am available for this ceremony."
	case "platform":
		return "android"
	case "device_id":
		return "android-device-001"
	case "ceremony_type":
		return "vivah"
	case "event_date":
		return "2026-06-15"
	case "address":
		return "12 MG Road, Bengaluru, Karnataka"
	case "description":
		return "Please include any ceremony, logistics, or family notes here."
	case "first_name":
		return "Synthetic"
	case "last_name":
		return "User"
	case "role":
		return "yajman"
	case "username":
		return "synthetic-user"
	case "email":
		return "user@example.com"
	default:
		return "string"
	}
}

func sampleNumber(name string, integer bool) any {
	switch name {
	case "latitude":
		return 12.9716
	case "longitude":
		return 77.5946
	case "rating":
		return 5
	case "limit":
		return 20
	case "budget", "spent":
		return 1000
	default:
		if integer {
			return 1
		}
		return 1.0
	}
}
