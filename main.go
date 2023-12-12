package main

import (
	_ "embed"
	"flag"
	"fmt"
	"github.com/flosch/go-humanize"
	"github.com/flosch/pongo2/v6"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/google/uuid"
	"github.com/sujit-baniya/flash"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed index.html
var indexView string

var validType = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/svg+xml":   true,
	"text/javascript": true,
	"text/css":        true,
}

func isValidFilename(filename string) bool {
	match, err := regexp.MatchString("^[A-Za-z0-9-_.]+$", filename)
	if err != nil {
		return false
	}
	return match
}

type DirInfo struct {
	Name  string     `json:"name"`
	Time  time.Time  `json:"time"`
	Files []FileInfo `json:"files"`
}
type FileInfo struct {
	Name string `json:"name"`
	Size string `json:"size"`
}

func getAccepts() string {
	accept := make([]string, 0)
	for k := range validType {
		accept = append(accept, k)
	}

	return strings.Join(accept, ",")
}
func readDirs(dir string) []DirInfo {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		log.Error("Error reading directory:", dir)
		return nil
	}

	// 获取文件信息
	fileInfos := make([]fs.FileInfo, 0, len(dirs))
	for _, file := range dirs {
		info, err1 := file.Info()
		if err1 != nil {
			log.Error("Error getting file info:", err1)
			return nil
		}
		fileInfos = append(fileInfos, info)
	}

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].ModTime().After(fileInfos[j].ModTime())
	})

	var result = make([]DirInfo, 0)
	for d := range fileInfos {
		info := fileInfos[d]
		id := info.Name()

		dirInfo := DirInfo{
			Name:  id,
			Files: make([]FileInfo, 0),
		}
		dirInfo.Time = info.ModTime()

		if files, e := os.ReadDir(dir + "/" + id); e == nil {
			for file := range files {
				fileInfo := FileInfo{
					Name: files[file].Name(),
				}
				if info, e1 := files[file].Info(); e1 == nil {
					fileInfo.Size = humanize.Bytes(uint64(info.Size()))
				}
				dirInfo.Files = append(dirInfo.Files, fileInfo)
			}
		}
		result = append(result, dirInfo)
	}
	return result
}

func Render(tpl *pongo2.Template, data map[string]any) string {
	out, err := tpl.Execute(data)
	if err != nil {
		log.Error(err)
		return ""
	}
	return out
}
func GetLogFileMust(f string) *os.File {
	logFile, e1 := os.OpenFile(f, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if e1 != nil {
		panic(e1)
	}
	return logFile
}
func main() {
	addr := flag.String("addr", ":3000", "HTTP服务地址")
	appLogFile := flag.String("appLogFile", "./app.log", "应用日志文件")
	reqLogFile := flag.String("reqLogFile", "./request.log", "请求日志文件")
	uploadPath := flag.String("uploadPath", "/tmp/uploads", "上传文件保存路径")
	maxFileSize := flag.Int64("maxFileSize", 2*1024*1024, "上传文件大小限制")
	urlPrefix := flag.String("urlPrefix", "http://localhost/static", "上传文件访问前缀")
	rootPass := flag.String("rootPass", "", "root密码")
	prd := flag.Bool("prd", true, "是否生产环境")

	flag.Parse()

	requestLogOutput := os.Stdout
	if *prd {
		appLog := GetLogFileMust(*appLogFile)
		log.SetOutput(appLog)
		requestLogOutput = GetLogFileMust(*reqLogFile)

		defer func() {
			_ = appLog.Close()
			_ = requestLogOutput.Close()
		}()
	}

	log.Info("--------------------------------------------------")
	log.Info("        addr: ", *addr)
	log.Info("  reqLogFile: ", *reqLogFile)
	log.Info("  appLogFile: ", *appLogFile)
	log.Info("  uploadPath: ", *uploadPath)
	log.Info(" maxFileSize: ", *maxFileSize)
	log.Info("   urlPrefix: ", *urlPrefix)
	log.Info("    rootPass: ", (*rootPass)[:3]+"***")
	log.Info("         prd: ", *prd)
	log.Info("--------------------------------------------------")

	if *rootPass == "" {
		panic("-rootPass is required")
	}
	if len(*rootPass) < 6 {
		panic("-rootPass is too short >= 6")
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logger.New(logger.Config{
		Format: "${time} ${ip}:${port} ${status} - ${user} ${method} ${path}\n",
		CustomTags: map[string]logger.LogFunc{
			"time": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return output.WriteString(time.Now().Format("2006/01/02 15:04:05.000000"))
			},
			"user": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return output.WriteString(c.Locals("username").(string))
			},
		},
		Output:        requestLogOutput,
		DisableColors: *prd,
	}))
	app.Use(basicauth.New(basicauth.Config{
		Users: map[string]string{
			"root": *rootPass,
		},
		//Unauthorized: func(c *fiber.Ctx) error {
		//	c.Status(fiber.StatusUnauthorized)
		//	return c.SendString("unauthorized")
		//},
	}))

	tpl, _ := pongo2.FromString(indexView)
	accept := getAccepts()

	app.Get("/", func(c *fiber.Ctx) error {
		data := fiber.Map{
			"url_prefix": urlPrefix,
			"data":       readDirs(*uploadPath),
			"accept":     accept,
			"prd":        prd,
		}
		flashData := flash.Get(c)

		if flashData["err"] != nil {
			data["err"] = flashData["err"]
		}
		if flashData["msg"] != nil {
			data["msg"] = flashData["msg"]
		}

		c.Set(fiber.HeaderContentType, fiber.MIMETextHTML)
		return c.SendString(Render(tpl, data))
	})
	app.Get("/req_log", func(c *fiber.Ctx) error {
		return c.SendStream(GetLogFileMust(*reqLogFile))
	})
	app.Get("/delete", func(c *fiber.Ctx) error {
		dir := c.Query("dir", "")
		file := c.Query("file", "")

		if dir != "" {
			targetDir := fmt.Sprintf("%s/%s", *uploadPath, dir)
			if file == "" {
				_ = os.RemoveAll(targetDir)
				log.Warn("rm_a: ", dir)
			} else {
				targetFile := fmt.Sprintf("%s/%s/%s", *uploadPath, dir, file)

				_ = os.Remove(targetFile)
				log.Warn("rm_f: ", dir, file)

				if par, err := os.ReadDir(targetDir); err == nil && len(par) == 0 {
					_ = os.Remove(targetDir)
					log.Warn("rm_d: ", dir)
				}
			}
		}
		return c.Redirect("/", fiber.StatusTemporaryRedirect)
	})
	app.Post("/", func(c *fiber.Ctx) error {
		data := fiber.Map{
			"url_prefix": urlPrefix,
			"accept":     accept,
			"prd":        prd,
		}

		form, err := c.MultipartForm()
		if err != nil {
			data["err"] = err.Error()
		}

		if len(form.File) < 1 {
			data["err"] = "No file uploaded"
		}
		if len(form.File) > 10 {
			data["err"] = "Too many files uploaded <= 10"
		}

		id := uuid.New().String()[:8]
		files := form.File["files"]

		for _, file := range files {
			cType := file.Header["Content-Type"][0]
			name := file.Filename
			size := file.Size

			if !validType[cType] {
				data["err"] = fmt.Sprintf("Invalid file type [%s] %s", cType, name)
				break
			}

			if !isValidFilename(name) {
				data["err"] = fmt.Sprintf("Invalid file name [%s], only a-z,0-9,-,_", name)
				break
			}

			if size > *maxFileSize {
				data["err"] = fmt.Sprintf("File size too large <2MB %s", name)
				break
			}
		}

		if data["err"] == nil {
			targetDir := fmt.Sprintf("%s/%s", *uploadPath, id)

			for {
				// ensure uuid prefix is unique in UPLOAD_PATH
				if _, exists := os.Stat(targetDir); !os.IsNotExist(exists) {
					id = uuid.New().String()[:8]
					targetDir = fmt.Sprintf("%s/%s", uploadPath, id)
				} else {
					break
				}
			}

			e := os.MkdirAll(targetDir, 0755)
			if e != nil {
				data["err"] = e.Error()
			}

			for _, file := range files {
				fail := c.SaveFile(file, fmt.Sprintf("%s/%s", targetDir, file.Filename))
				if fail != nil {
					data["err"] = fail.Error()
					break
				}
			}

			if data["err"] != nil {
				data["msg"] = fmt.Sprintf("Upload %d files success under %s", len(files), id)
			}
		}

		flash.WithData(c, data)
		return c.Redirect("/", fiber.StatusSeeOther)
	})

	log.Infof("Server started at http://localhost%s\n", *addr)
	app.Listen(*addr)
}
