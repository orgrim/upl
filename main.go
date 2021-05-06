package main

import (
	"embed"
	"flag"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

var version string = "0.1.0"

// A config is the whole application configuration struct based on defaults and
// user input from commandline options
type config struct {
	// Embed template and static files
	NoEmbed bool
	// Path to the directory where to list and upload files
	StoreDir string
}

// newConfig creates the default configuration struct
func newConfig() config {
	return config{
		NoEmbed:  false,
		StoreDir: "files",
	}
}

// parseCli processes command line arguments and returns the configuration
func parseCli(args []string) config {
	c := newConfig()

	flag.CommandLine = flag.NewFlagSet(args[0], flag.ExitOnError)

	noEmbed := flag.Bool("no-embed", c.NoEmbed, "serve template and static dir from cwd")
	storeDir := flag.String("store", c.StoreDir, "destination dir of uploads")
	showVersion := flag.Bool("version", false, "show version")
	showHelp := flag.Bool("help", false, "print help")

	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Println("uploader version", version)
		os.Exit(0)
	}

	c.NoEmbed = *noEmbed
	c.StoreDir = *storeDir

	return c
}

//go:embed static
var staticFS embed.FS

func selectStaticFS(noEmbed bool) (fs.FS, error) {
	if noEmbed {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		sp := filepath.Join(cwd, "static")
		return os.DirFS(sp), nil
	}
	subfs, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	return subfs, nil
}

//go:embed tpl
var tplFS embed.FS

func selectTplFS(noEmbed bool) (fs.FS, error) {
	if noEmbed {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		sp := filepath.Join(cwd, "tpl")
		return os.DirFS(sp), nil
	}
	subfs, err := fs.Sub(tplFS, "tpl")
	if err != nil {
		return nil, err
	}
	return subfs, nil
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func app(conf config) error {
	// Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "${time_rfc3339} ${remote_ip} ${latency_human} ${method} ${uri} ${status} ${error}\n",
	}))
	e.Use(middleware.Recover())

	// Templates from tpl
	tplfs, err := selectTplFS(conf.NoEmbed)
	if err != nil {
		return err
	}

	t := &Template{
		templates: template.Must(template.ParseFS(tplfs, "*.html")),
	}

	e.Renderer = t

	stFS, err := selectStaticFS(conf.NoEmbed)
	if err != nil {
		return err
	}

	// Routes
	e.GET("/", uplWrapHandler(listFiles, conf))
	e.POST("/", uplWrapHandler(uploadFiles, conf))
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(stFS)))))

	e.Static("/files", conf.StoreDir)

	// Start server
	err = e.Start(":1323")
	e.Logger.Fatal(err)

	return err
}

func main() {
	conf := parseCli(os.Args)

	_, err := os.Stat(conf.StoreDir)
	if err != nil {
		if err := os.MkdirAll(conf.StoreDir, 0755); err != nil {
			log.Fatalln(err)
		}
	}

	err = app(conf)
	if err != nil {
		log.Fatalln(err)
	}
}

// Handler
func uplWrapHandler(uf func(echo.Context, config) error, conf config) echo.HandlerFunc {
	return func(c echo.Context) error { return uf(c, conf) }
}

func listFiles(c echo.Context, conf config) error {

	v := struct {
		Title string
		Files []string
	}{
		Title: "Uploader",
		Files: listCurrentDir(conf.StoreDir),
	}

	return c.Render(http.StatusOK, "main.html", v)
}

func uploadFiles(c echo.Context, conf config) error {

	form, err := c.MultipartForm()
	if err != nil {
		return err
	}
	files := form.File["upload"]

	for _, file := range files {

		// Source
		src, err := file.Open()
		if err != nil {
			return err
		}
		defer src.Close()

		filename := filepath.Base(filepath.Clean(file.Filename))
		if filename == "." || filename == "/" {
			return fmt.Errorf("invalid filename")
		}
		filename = filepath.Join(conf.StoreDir, filename)

		dst, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return nil
		}
	}

	return listFiles(c, conf)
}

func listCurrentDir(dir string) []string {
	des, err := os.ReadDir(dir)
	if err != nil {
		log.Println("could not read current directory:", err)
		return []string{}
	}
	f := make([]string, 0, len(des))
	for _, e := range des {
		f = append(f, e.Name())
	}
	return f
}
