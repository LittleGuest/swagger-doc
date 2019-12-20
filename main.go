package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/LittleGuest/tool"
	"github.com/gorilla/mux"
	"github.com/unidoc/unioffice/color"
	"github.com/unidoc/unioffice/document"
	"github.com/unidoc/unioffice/measurement"
	"github.com/unidoc/unioffice/schema/soo/wml"
	"github.com/unidoc/unioffice/spreadsheet"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	SERVER_LOG_FILE      = "server_log.log"
	READ_TIMEOUT         = 15
	WRITE_TIMEOUT        = 15
	IDLE_TIMEOUT         = 15
	FILE_SUFFIX_XLSX     = "xlsx"
	FILE_SUFFIX_HTML     = "html"
	FILE_SUFFIX_MARKDOWN = "md"
	FILE_SUFFIX_PDF      = "pdf"
	FILE_SUFFIX_WORD     = "word"
	FILE_NAME            = "api"
	TEMP_FILE_NAME       = "api-test"
)

var (
	logger *log.Logger
)

// 记录请求时间
func RequestTimeMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		handler.ServeHTTP(w, r)
		end := time.Now()
		logger.Printf("%v\t%v, 耗时：%v", r.Method, r.RequestURI, end.Sub(start))
	})
}

// 跨域中间件
func CorsMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
		}
		handler.ServeHTTP(w, r)
	})
}

func main() {
	// 记录日志
	logFile, err := os.OpenFile(SERVER_LOG_FILE, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("打开日志文件失败：%v", err)
		return
	}
	defer logFile.Close()

	// 自定义日志
	logger = log.New(logFile, "[INFO]\t", log.LstdFlags)

	router := mux.NewRouter()
	// 使用中间件，跨域中间件，记录请求时间中间件
	router.Use(CorsMiddleware, RequestTimeMiddleware)

	router.HandleFunc("/api/v1/api", GetApi).Methods(http.MethodGet)
	router.HandleFunc("/api/v1/to-file", CreateFile).Methods(http.MethodGet)

	// 静态文件服务
	router.PathPrefix("").Handler(http.StripPrefix("", http.FileServer(http.Dir("views"))))

	server := &http.Server{
		Addr:         ":65520",
		Handler:      router,
		ReadTimeout:  READ_TIMEOUT * time.Second,
		WriteTimeout: WRITE_TIMEOUT * time.Second,
		IdleTimeout:  IDLE_TIMEOUT * time.Second,
		ErrorLog:     logger,
	}

	// 优雅的关闭服务
	doneChannel := make(chan bool)
	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, os.Interrupt, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-quitChannel
		logger.Println("服务关闭。。。")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Fatalf("异常关闭服务: %v\n", err)
		}
		close(doneChannel)
	}()

	logger.Println("server already listen at...")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("服务启动失败：%v\n", err)
	}
	<-doneChannel
	logger.Println("主程序退出。。。")
}

// 获取json
func GetApi(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if tool.IsBlank(url) {
		http.Error(w, "路径参数为空", http.StatusBadRequest)
		return
	}
	swaggerApi, err := AnalysisApiJson(url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(swaggerApi.String()))
}

// 创建文件
func CreateFile(w http.ResponseWriter, r *http.Request) {
	fileType := r.URL.Query().Get("type")
	url := r.URL.Query().Get("url")
	if tool.IsBlank(fileType) || tool.IsBlank(url) {
		logger.Println("参数为空")
		http.Error(w, "参数为空", http.StatusBadRequest)
		return
	}

	var fileName string
	var resp []byte
	switch fileType {
	case "excel":
		swaggerApi, err := AnalysisApiJson(url)
		if err != nil {
			logger.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ToExcel(swaggerApi)
		resp, _ = ioutil.ReadFile("api-test.xlsx")
		removeFile("api-test.xlsx")
		fileName = "api.xlsx"
	case "html":
	case "md":
		swaggerApi, err := AnalysisApiJson(url)
		if err != nil {
			logger.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ToMarkdown(swaggerApi)
		resp, _ = ioutil.ReadFile("api-test.zip")
		removeFile("api-test.zip")
		fileName = "api.zip"
	case "pdf":
	case "word":
		swaggerApi, err := AnalysisApiJson(url)
		if err != nil {
			logger.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ToWord(swaggerApi)
		resp, _ = ioutil.ReadFile("api-test.docx")
		go func() {
			removeFile("api-test.docx")
		}()
		fileName = "api.docx"
	default:
		_, _ = w.Write([]byte("暂时不支持该文件类型"))
	}

	w.Header().Set("Content-Disposition", "attachment;filename="+fileName)
	_, _ = w.Write(resp)
}

// TODO 转excel
func ToExcel(swaggerApi SwaggerApi) {
	wb := spreadsheet.New()
	sheet := wb.AddSheet()
	for r := 0; r < 5; r++ {
		row := sheet.AddRow()
		for c := 0; c < 5; c++ {
			cell := row.AddCell()
			cell.SetString(fmt.Sprintf("row %d cell %d", r, c))
		}
	}
	if err := wb.Validate(); err != nil {
		logger.Fatalf("error validating sheet: %s", err)
	}
	if err := wb.SaveToFile("api-test.xlsx"); err != nil {
		logger.Fatalf("excel文件创建失败：%v", err)
	}
}

// TODO 转html

// 转markdown, 一个接口一个文档，打包压缩下载
func ToMarkdown(swaggerApi SwaggerApi) {
	zipFile, err := os.Create("api-test.zip")
	defer zipFile.Close()
	if err != nil {
		logger.Printf("创建压缩文件失败：%v", err)
		return
	}
	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	for key, value := range swaggerApi.Paths {
		for kk, vv := range value {
			data := make(map[string]interface{})
			data["Summary"] = vv.Summary
			data["Description"] = vv.Description
			data["Path"] = key
			data["Method"] = kk
			data["Consumes"] = strings.Join(vv.Consumes, ",")
			data["Produces"] = strings.Join(vv.Produces, ",")
			data["Parameter"] = vv.Parameters
			data["Response"] = vv.Responses

			temp := template.Must(template.New("markdown.html").ParseFiles("template/markdown.html"))
			w, err := zw.Create(strings.ReplaceAll(vv.Summary, " ", "_") + ".md")
			if err != nil {
				logger.Printf("创建文件失败：%v", err)
				continue
			}
			if err := temp.Execute(w, data); err != nil {
				logger.Printf("写入模板文件失败：%v", err)
				continue
			}
		}
	}
}

// TODO 转pdf

// 转word
func ToWord(swaggerApi SwaggerApi) {
	doc := document.New()
	run := doc.AddParagraph().AddRun()
	run.AddText("接口文档")
	run.Properties().SetBold(true)
	run.Properties().SetSize(24 * measurement.Point)

	doc.AddParagraph()
	doc.AddParagraph()

	for key, value := range swaggerApi.Paths {
		for kk, vv := range value {
			paragraph := doc.AddParagraph()
			run := paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("接口名称")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(vv.Summary)

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("接口描述")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(vv.Description)

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("接口地址")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(key)

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("请求方式")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(kk)

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("consumes")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(strings.Join(vv.Consumes, ","))

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("produces")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText(strings.Join(vv.Produces, ","))

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("请求示例")
			run.AddTab()
			run = paragraph.AddRun()
			run.Properties().SetColor(color.OrangeRed)
			run.AddText("请求示例")

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("请求参数")
			table := doc.AddTable()
			table.Properties().SetWidthPercent(100)
			table.Properties().Borders().SetAll(wml.ST_BorderSingle, color.Auto, measurement.Zero)
			row := table.AddRow()
			row.AddCell().AddParagraph().AddRun().AddText("参数名称")
			row.AddCell().AddParagraph().AddRun().AddText("参数说明")
			row.AddCell().AddParagraph().AddRun().AddText("请求类型")
			row.AddCell().AddParagraph().AddRun().AddText("是否必须")
			row.AddCell().AddParagraph().AddRun().AddText("数据类型")
			row.AddCell().AddParagraph().AddRun().AddText("schema")
			for _, parameter := range vv.Parameters {
				row = table.AddRow()
				row.AddCell().AddParagraph().AddRun().AddText(parameter.Name)
				row.AddCell().AddParagraph().AddRun().AddText(parameter.Description)
				row.AddCell().AddParagraph().AddRun().AddText(parameter.In)
				row.AddCell().AddParagraph().AddRun().AddText(strconv.FormatBool(parameter.Required))
				row.AddCell().AddParagraph().AddRun().AddText("")
				row.AddCell().AddParagraph().AddRun().AddText("")
			}

			doc.AddParagraph()

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("响应状态")
			table = doc.AddTable()
			table.Properties().SetWidthPercent(100)
			table.Properties().Borders().SetAll(wml.ST_BorderSingle, color.Auto, measurement.Zero)
			row = table.AddRow()
			row.AddCell().AddParagraph().AddRun().AddText("状态码")
			row.AddCell().AddParagraph().AddRun().AddText("说明")
			row.AddCell().AddParagraph().AddRun().AddText("schema")
			for responseStatus, response := range vv.Responses {
				row = table.AddRow()
				row.AddCell().AddParagraph().AddRun().AddText(responseStatus)
				row.AddCell().AddParagraph().AddRun().AddText(response.Description)
				row.AddCell().AddParagraph().AddRun().AddText("")
			}

			doc.AddParagraph()

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("响应参数")
			table = doc.AddTable()
			table.Properties().SetWidthPercent(100)
			table.Properties().Borders().SetAll(wml.ST_BorderSingle, color.Auto, measurement.Zero)
			row = table.AddRow()
			row.AddCell().AddParagraph().AddRun().AddText("参数名称")
			row.AddCell().AddParagraph().AddRun().AddText("参数说明")
			row.AddCell().AddParagraph().AddRun().AddText("类型")
			row.AddCell().AddParagraph().AddRun().AddText("schema")

			doc.AddParagraph()

			paragraph = doc.AddParagraph()
			run = paragraph.AddRun()
			run.Properties().SetBold(true)
			run.Properties().SetSize(12 * measurement.Point)
			run.AddText("响应示例")

			doc.AddParagraph()
			doc.AddParagraph()
		}
	}

	if err := doc.Validate(); err != nil {
		logger.Fatalf("word文档验证失败：%v", err)
	}

	if err := doc.SaveToFile("api-test.docx"); err != nil {
		logger.Fatalln(err)
	}
}

// 读取解析API JSON 数据
func AnalysisApiJson(url string) (sa SwaggerApi, err error) {
	if tool.IsBlank(url) {
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		logger.Printf("获取API JSON数据失败: %s", err)
		return
	}
	body := resp.Body
	defer body.Close()
	data, err := ioutil.ReadAll(body)
	if err != nil {
		logger.Printf("读取API JSON数据失败: %s", err)
		return
	}
	if err = json.Unmarshal(data, &sa); err != nil {
		logger.Printf("解析API JSON数据失败：%s", err)
		return
	}
	return
}

// 删除文件
func removeFile(fileName string) bool {
	if tool.IsBlank(fileName) {
		return true
	}
	if err := os.Remove(fileName); err != nil {
		logger.Printf("删除文件失败：%v", err)
		return true
	}
	return false
}

// 响应信息
type ResponseInfo struct {
	Description string                 `json:"description"` // 描述
	Schema      map[string]interface{} `json:"schema"`
	Headers     map[string]interface{} `json:"headers"`
}

func (ri ResponseInfo) String() string {
	resp, _ := json.Marshal(ri)
	return string(resp)
}

func (ri ResponseInfo) StringHeaders() string {
	resp, _ := json.Marshal(ri.Headers)
	return string(resp)
}

func (ri ResponseInfo) StringSchema() string {
	resp, _ := json.Marshal(ri.Schema)
	return string(resp)
}

// 参数信息
type Parameter struct {
	Name        string                 `json:"name"`        // 名称
	In          string                 `json:"in"`          // 位置
	Description string                 `json:"description"` // 描述
	Required    bool                   `json:"required"`    // 是否必填
	Type        string                 `json:"type"`        // 类型
	Format      string                 `json:"format"`      // 类型
	Maximum     int64                  `json:"maximum"`     // 最大值
	Minimum     int64                  `json:"minimum"`     // 最小值
	Schema      map[string]interface{} `json:"schema"`
	Items       struct {
		Type                 string                 `json:"type"`
		Enum                 []string               `json:"enum"`
		Default              string                 `json:"default"`
		AdditionalProperties map[string]interface{} `json:"additionalProperties"`
	} `json:"items"`
	CollectionFormat string `json:"collectionFormat"`
}

func (p Parameter) String() string {
	resp, _ := json.Marshal(p)
	return string(resp)
}

// 路由信息
type PathInfo struct {
	Tags        []string                `json:"tags"`        // 分组标签
	Summary     string                  `json:"summary"`     // 概要
	Description string                  `json:"description"` // 描述
	OperationId string                  `json:"operationId"` // 操作
	Consumes    []string                `json:"consumes"`
	Produces    []string                `json:"produces"` // 返回格式
	Parameters  []Parameter             `json:"parameters"`
	Responses   map[string]ResponseInfo `json:"responses"`
	Security    []map[string][]interface{} `json:"security"` // 认证信息
}

func (p PathInfo) String() string {
	resp, _ := json.Marshal(p)
	return string(resp)
}

// 外部文档
type ExternalDocs struct {
	Description string `json:"description"` // 描述
	Url         string `json:"url"`         // 地址
}

func (ed ExternalDocs) String() string {
	resp, _ := json.Marshal(ed)
	return string(resp)
}

// 标签分组信息
type Tag struct {
	Name         string       `json:"name"`        // 名称
	Description  string       `json:"description"` // 描述
	ExternalDocs ExternalDocs `json:"externalDocs"`
}

func (t Tag) String() string {
	resp, _ := json.Marshal(t)
	return string(resp)
}

// swagger文档信息
type SwaggerInfo struct {
	Description    string `json:"description"`    // 描述
	Version        string `json:"version"`        // 版本
	Title          string `json:"title"`          // 标题
	TermsOfService string `json:"termsOfService"` // 使用条款
	Contact        struct {
		Name  string `json:"name"`  // 名称
		Email string `json:"email"` // 邮箱
		Url   string `json:"url"`   // 地址
	} `json:"contact"` // 联系信息
	License struct {
		Name string `json:"name"` // 名称
		Url  string `json:"url"`  // 地址
	} `json:"license"` // 许可证
}

func (si SwaggerInfo) String() string {
	resp, _ := json.Marshal(si)
	return string(resp)
}

// 返回的对象信息
type Definition struct {
	Type       string                 `json:"type"`       // 类型
	Properties map[string]interface{} `json:"properties"` // 属性
	Xml        map[string]interface{} `json:"xml"`        // xml 名称
	Required   []string               `json:"required"`   // 必填属性
}

func (d Definition) String() string {
	resp, _ := json.Marshal(d)
	return string(resp)
}

// SwaggerApi
type SwaggerApi struct {
	Swagger             string                         `json:"swagger"` // swagger版本
	Info                SwaggerInfo                    `json:"info"`
	Host                string                         `json:"host"`     // 地址
	BasePath            string                         `json:"basePath"` // 路径
	Tags                []Tag                          `json:"tags"`
	Schemes             []string                       `json:"schemes"`
	Paths               map[string]map[string]PathInfo `json:"paths"`
	SecurityDefinitions map[string]interface{}         `json:"securityDefinitions"`
	Definitions         map[string]Definition          `json:"definitions"`
	ExternalDocs        ExternalDocs                   `json:"externalDocs"`
}

func (sa SwaggerApi) String() string {
	resp, _ := json.Marshal(sa)
	return string(resp)
}
