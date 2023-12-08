package main

import (
	"fmt"
	"github.com/flosch/go-humanize"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/django/v3"
	"github.com/google/uuid"
	"os"
	"regexp"
	"time"
)

const MAX_FILE_SIZE = 2 * 1024 * 1024
const UPLOAD_PATH = "/tmp/uploads"
const URL_PREFIX = "http://localhost:3000"

var validType = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/svg+xml": true,
}

func isValidFilename(filename string) bool {
	match, err := regexp.MatchString("^[A-Za-z0-9-_.]+$", filename)
	if err != nil {
		return false
	}
	return match
}

//{
//	"name": "uuid",
//	"files": [
//	    { name: "filename", "size": 123 }
//	]
//}

type DirInfo struct {
	Name  string     `json:"name"`
	Time  time.Time  `json:"time"`
	Files []FileInfo `json:"files"`
}
type FileInfo struct {
	Name string `json:"name"`
	Size string `json:"size"`
}

func readDirs(dir string) []DirInfo {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	var result = make([]DirInfo, 0)

	for d := range dirs {
		id := dirs[d].Name()
		dirInfo := DirInfo{
			Name:  id,
			Files: make([]FileInfo, 0),
		}
		if info, e0 := dirs[d].Info(); e0 == nil {
			dirInfo.Time = info.ModTime()
		}
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

func main() {

	engine := django.New("./", ".html")

	engine.ShouldReload = true
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Get("/delete", func(c *fiber.Ctx) error {
		dir := c.Query("dir", "")
		file := c.Query("file", "")

		if dir != "" {
			targetDir := fmt.Sprintf("%s/%s", UPLOAD_PATH, dir)
			if file == "" {
				_ = os.RemoveAll(targetDir)
				fmt.Println("rm_a", dir)
			} else {
				targetFile := fmt.Sprintf("%s/%s/%s", UPLOAD_PATH, dir, file)

				_ = os.Remove(targetFile)
				fmt.Println("rm_f", dir, file)

				if par, err := os.ReadDir(targetDir); err == nil && len(par) == 0 {
					_ = os.Remove(targetDir)
					fmt.Println("rm_d", dir)
				}
			}
		}
		return c.Redirect("/", fiber.StatusTemporaryRedirect)
	})
	app.Get("/", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTML)
		return c.Render("home", fiber.Map{
			"url_prefix": URL_PREFIX,
			"data":       readDirs(UPLOAD_PATH),
		})
	})
	app.Post("/", func(c *fiber.Ctx) error {
		data := fiber.Map{
			"url_prefix": URL_PREFIX,
		}

		form, err := c.MultipartForm()
		if err != nil {
			data["err"] = err.Error()
		}

		if len(form.File) < 1 {
			data["err"] = "No file uploaded"
		}

		id := uuid.New().String()[:8]
		files := form.File["files"]

		for _, file := range files {
			cType := file.Header["Content-Type"][0]
			name := file.Filename

			if !validType[cType] {
				data["err"] = fmt.Sprintf("Invalid file type [%s] %s", cType, name)
				break
			}

			if !isValidFilename(name) {
				data["err"] = fmt.Sprintf("Invalid file name [%s], only a-z,0-9,-,_", name)
				break
			}

			if file.Size > MAX_FILE_SIZE {
				data["err"] = fmt.Sprintf("File size too large <2MB %s", name)
				break
			}
		}

		if data["err"] == nil {
			targetDir := fmt.Sprintf("%s/%s", UPLOAD_PATH, id)

			for {
				// ensure uuid prefix is unique in UPLOAD_PATH
				if _, exists := os.Stat(targetDir); !os.IsNotExist(exists) {
					id = uuid.New().String()[:8]
					targetDir = fmt.Sprintf("%s/%s", UPLOAD_PATH, id)
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

		data["data"] = readDirs(UPLOAD_PATH)

		c.Set(fiber.HeaderContentType, fiber.MIMETextHTML)
		return c.Render("home", data)
	})

	app.Listen(":3000")
}
