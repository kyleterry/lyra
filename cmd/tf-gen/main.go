package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/terraform-providers/terraform-provider-aws/aws"
)

var providerTemplate = `

// {{.TitleType}}Handler ...
type {{.TitleType}}Handler struct {
	provider *schema.Provider
}

// Create ...
func (h *{{.TitleType}}Handler) Create(desired *{{.TitleType}}) (*{{.TitleType}}, string, error) {
	rState := {{.TitleType}}Mapper(desired)
	id, err := bridge.Create(h.provider, "{{.TFType}}", rState)
	if err != nil {
		return nil, "", err
	}
	actual, err := h.Read(id)
	if err != nil {
		return nil, "", err
	}
	return actual, id, nil
}

// Read ...
func (h *{{.TitleType}}Handler) Read(externalID string) (*{{.TitleType}}, error) {
	actual, err := bridge.Read(h.provider, "{{.TFType}}", externalID)
	if err != nil {
		return nil, err
	}
	return {{.TitleType}}Unmapper(actual), nil
}

// Delete ...
func (h *{{.TitleType}}Handler) Delete(externalID string) error {
	return bridge.Delete(h.provider, "{{.TFType}}", externalID)
}

`

var mapperPrefix = `
func %sMapper(r *%s) *terraform.ResourceConfig {
	config := map[string]interface{}{}
 	`

var mapperSuffix = `return &terraform.ResourceConfig{
		Config: config,
	}
}
`

var unmapperPrefix = `
func %sUnmapper(state map[string]interface{}) *%s {
	r := &%s{}
`

var unmapper = `
if x, ok := state["%s"]; ok {
	r.%s = x.(%s)
}
`

var tagsUnmapper = `
if x, ok := state["%s"]; ok {
	r.%s = convertMap(x.(map[string]interface{}))
}
`

var unmapperWithPointerDeref = `
if x, ok := state["%s"]; ok {
	x := x.(%s)
	r.%s = &x
}
`

var tagsUnmapperWithPointerDeref = `
if x, ok := state["%s"]; ok {
	x := convertMap(x.(map[string]interface{}))
	r.%s = &x
}
`

var unmapperSuffix = `	return r
}
`

var prefix = `// Code generated by Lyra DO NOT EDIT.

// This code is generated on a per-provider basis using "tf-gen"
// Long term our hope is to remove this generation step and adopt dynamic approach

package generated

import (
	"%s"
	"github.com/lyraproj/puppet-evaluator/eval"
	"github.com/lyraproj/servicesdk/service"
	"github.com/hashicorp/terraform/helper/schema"
 	"github.com/hashicorp/terraform/terraform"
)

func convertMap(in map[string]interface{}) map[string]string {
	m  := map[string]string{}
	for k,v := range in {
		m[k] = v.(string)
	}
	return m
}


func unconvertMap(in map[string]string  ) map[string]interface{} {
	m  := map[string]interface{}{}
	for k,v := range in {
		m[k] = v
	}
	return m
}

`

func getGoType(key string, s *schema.Schema) string {
	// FIXME many missing types including all maps except tags ...
	var t string
	switch s.Type {
	case schema.TypeString:
		t = "string"
	case schema.TypeMap:
		if key == "tags" {
			t = "map[string]string"
		}
	case schema.TypeBool:
		t = "bool"
	default:
		return ""
	}
	return t
}

func generateResource(rType string, r *schema.Resource) {
	fmt.Printf("type %s struct {\n", rType)
	id := fmt.Sprintf("%s_id", rType)
	fmt.Printf("     %s *string\n", id)
	for k, v := range r.Schema {
		if k == "type" {
			k = "resource_type"
		}
		goType := getGoType(k, v)
		if goType == "" {
			log.Printf("Ignoring unsupported schema: %s -> %s -> %v", rType, k, v.Type)
			continue
		}
		if !v.Required {
			goType = "*" + goType
		}
		fmt.Println("    ", strings.Title(k), goType)
	}
	fmt.Printf("}\n\n")
}

func generateMapper(rType string, r *schema.Resource) {
	fmt.Printf(mapperPrefix, rType, rType)
	for k, v := range r.Schema {
		if k == "type" {
			k = "resource_type"
		}
		if getGoType(k, v) == "" {
			log.Printf("Ignoring unsupported schema: %s -> %s -> %v", rType, k, v.Type)
			continue
		}
		if v.Type == schema.TypeMap {
			if v.Required {
				fmt.Printf("    config[\"%s\"] = unconvertMap(r.%s)\n", k, strings.Title(k))
			} else {
				fmt.Printf("if r.%s != nil {\n", strings.Title(k))
				fmt.Printf("    config[\"%s\"] = unconvertMap(*r.%s)\n", k, strings.Title(k))
				fmt.Printf("}\n")
			}
		} else {
			if v.Required {
				fmt.Printf("    config[\"%s\"] = r.%s\n", k, strings.Title(k))
			} else {
				fmt.Printf("if r.%s != nil {\n", strings.Title(k))
				fmt.Printf("    config[\"%s\"] = *r.%s\n", k, strings.Title(k))
				fmt.Printf("}\n")
			}
		}
	}
	fmt.Printf(mapperSuffix)
}

func generateUnmapper(rType string, r *schema.Resource) {
	fmt.Printf(unmapperPrefix, rType, rType, rType)
	id := fmt.Sprintf("%s_id", rType)
	fmt.Printf(unmapperWithPointerDeref, "external_id", "string", strings.Title(id))
	for k, v := range r.Schema {
		if k == "type" {
			k = "resource_type"
		}
		goType := getGoType(k, v)
		if goType == "" {
			log.Printf("Ignoring unsupported schema: %s -> %s -> %v", rType, k, v.Type)
			continue
		}
		if v.Type == schema.TypeMap {
			if v.Required {
				fmt.Printf(tagsUnmapper, k, strings.Title(k))
			} else {
				fmt.Printf(tagsUnmapperWithPointerDeref, k, strings.Title(k))
			}
		} else {
			if v.Required {
				fmt.Printf(unmapper, k, strings.Title(k), goType)
			} else {
				fmt.Printf(unmapperWithPointerDeref, k, goType, strings.Title(k))
			}
		}
	}
	fmt.Printf(unmapperSuffix)
}

type providerType struct {
	TitleType string
	TFType    string
}

func generateProvider(rType string) {
	tmpl := template.Must(template.New("provider").Parse(providerTemplate))
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, providerType{strings.Title(rType), rType})
	if err != nil {
		panic(err)
	}
	fmt.Printf(buf.String())
}

func writeFile(filename, data string) {
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	_, err = f.WriteString(data)
	if err != nil {
		panic(err)
	}
}

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Please specify the package name of the bridge e.g. \"github.com/lyraproj/lyra/cmd/goplugin-tf-aws/bridge\"")
		os.Exit(1)
	}

	packageName := os.Args[1]
	fmt.Printf(prefix, packageName)
	fmt.Printf("func Initialize(sb *service.ServerBuilder, p *schema.Provider) {\n")
	fmt.Printf("    var evs []eval.Type\n")

	p := aws.Provider().(*schema.Provider)
	rTypes := make([]string, len(p.ResourcesMap))
	i := 0
	for k := range p.ResourcesMap {
		rTypes[i] = k
		i++
	}
	rTypes = sort.StringSlice(rTypes)

	for _, rType := range rTypes {
		// if rType != "aws_vpc" && rType != "aws_subnet" {
		// 	continue
		// }
		rTitleType := strings.Title(rType)
		fmt.Printf("    evs = sb.RegisterTypes(\"AwsTerraform\", %s{})\n", rTitleType)
		fmt.Printf("    sb.RegisterHandler(\"AwsTerraform::%sHandler\", &%sHandler{provider: p}, evs[0])\n", rTitleType, rTitleType)
	}
	fmt.Printf("}\n\n")

	for _, rType := range rTypes {
		// if rType != "aws_vpc" && rType != "aws_subnet" {
		// 	continue
		// }
		r := p.ResourcesMap[rType]
		generateResource(strings.Title(rType), r)
		generateMapper(strings.Title(rType), r)
		generateUnmapper(strings.Title(rType), r)
		generateProvider(rType)
	}

}