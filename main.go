package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GitHub API 配置
type GitHubConfig struct {
	Token      string
	Owner      string
	Repo       string
	Branch     string
	ImagesPath string
	UseCDN     bool
}

// 上传图片响应
type UploadResponse struct {
	Content struct {
		DownloadURL string `json:"download_url"`
	} `json:"content"`
}

// 全局变量，用于跟踪是否已检查目录
var directoryChecked bool = false

// 处理结果
type ProcessResult struct {
	OriginalFile  string
	ProcessedFile string
	ImageCount    int
	Success       bool
	Error         string
}

func main() {
	// 命令行参数
	port := flag.String("port", "8080", "Port to run the web server on")
	token := flag.String("token", "", "GitHub personal access token")
	owner := flag.String("owner", "", "GitHub repository owner")
	repo := flag.String("repo", "", "GitHub repository name")
	branch := flag.String("branch", "main", "GitHub repository branch")
	imagesPath := flag.String("images-path", "images", "Path in the repository to store images")
	useCDN := flag.Bool("use-cdn", true, "Use jsDelivr CDN for image URLs")
	flag.Parse()

	// 验证必要参数
	if *token == "" || *owner == "" || *repo == "" {
		flag.Usage()
		os.Exit(1)
	}

	config := GitHubConfig{
		Token:      *token,
		Owner:      *owner,
		Repo:       *repo,
		Branch:     *branch,
		ImagesPath: *imagesPath,
		UseCDN:     *useCDN,
	}

	// 创建临时目录用于存储上传的文件
	tempDir, err := ioutil.TempDir("", "mdimg2hub")
	if err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)



	// 设置路由
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderTemplate(w, "index.html", nil)
	})

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		handleUpload(w, r, tempDir, config)
	})

	http.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		handleDownload(w, r, tempDir)
	})

	// 启动服务器
	log.Printf("Starting server on port %s...", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

// 渲染HTML模板
func renderTemplate(w http.ResponseWriter, tmplName string, data interface{}) {
	tmplPath := filepath.Join("templates", tmplName)
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// 处理文件上传
func handleUpload(w http.ResponseWriter, r *http.Request, tempDir string, config GitHubConfig) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单
	err := r.ParseMultipartForm(32 << 20) // 32MB max memory
	if err != nil {
		sendJSONError(w, "Failed to parse form", err)
		return
	}

	// 获取上传的文件
	file, handler, err := r.FormFile("zipFile")
	if err != nil {
		sendJSONError(w, "Failed to get uploaded file", err)
		return
	}
	defer file.Close()

	// 检查文件类型
	if !strings.HasSuffix(strings.ToLower(handler.Filename), ".zip") {
		sendJSONError(w, "Only ZIP files are supported", nil)
		return
	}

	// 创建临时文件
	zipPath := filepath.Join(tempDir, handler.Filename)
	tempFile, err := os.Create(zipPath)
	if err != nil {
		sendJSONError(w, "Failed to create temporary file", err)
		return
	}
	defer tempFile.Close()

	// 复制上传的文件到临时文件
	_, err = io.Copy(tempFile, file)
	if err != nil {
		sendJSONError(w, "Failed to save uploaded file", err)
		return
	}

	// 创建解压目录
	extractDir := filepath.Join(tempDir, "extract_"+filepath.Base(handler.Filename))
	err = os.MkdirAll(extractDir, 0755)
	if err != nil {
		sendJSONError(w, "Failed to create extraction directory", err)
		return
	}

	// 解压文件
	err = unzipFile(zipPath, extractDir)
	if err != nil {
		sendJSONError(w, "Failed to extract ZIP file", err)
		return
	}

	// 查找所有 Markdown 文件
	var mdFiles []string
	err = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".md") {
			mdFiles = append(mdFiles, path)
		}
		return nil
	})

	if err != nil {
		sendJSONError(w, "Failed to search for Markdown files", err)
		return
	}

	if len(mdFiles) == 0 {
		sendJSONError(w, "No Markdown files found in the ZIP archive", nil)
		return
	}

	// 处理第一个 Markdown 文件
	mdFile := mdFiles[0]
	result, err := processMarkdownFile(mdFile, config)
	if err != nil {
		sendJSONError(w, "Failed to process Markdown file", err)
		return
	}

	// 返回成功响应
	response := ProcessResult{
		OriginalFile:  filepath.Base(mdFile),
		ProcessedFile: result.outputPath,
		ImageCount:    result.imageCount,
		Success:       true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// 处理下载请求
func handleDownload(w http.ResponseWriter, r *http.Request, tempDir string) {
	// 获取文件路径参数
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "No file specified", http.StatusBadRequest)
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(filePath)))
	w.Header().Set("Content-Type", "application/octet-stream")

	// 提供文件下载
	http.ServeFile(w, r, filePath)
}

// 发送JSON错误响应
func sendJSONError(w http.ResponseWriter, message string, err error) {
	errMsg := message
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", message, err)
	}

	response := ProcessResult{
		Success: false,
		Error:   errMsg,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// 解压ZIP文件
func unzipFile(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// 安全检查，防止目录遍历攻击
		filePath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filePath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return err
		}

		// 创建文件
		outFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		// 打开zip中的文件
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		// 复制内容
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// 处理结果
type processResult struct {
	outputPath string
	imageCount int
}

// 处理单个 Markdown 文件
func processMarkdownFile(filePath string, config GitHubConfig) (*processResult, error) {
	// 读取文件内容
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", filePath, err)
	}

	// 查找所有图片引用
	re := regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)
	matches := re.FindAllStringSubmatchIndex(string(content), -1)

	// 如果没有图片引用，直接返回
	if len(matches) == 0 {
		// 创建输出文件
		ext := filepath.Ext(filePath)
		baseName := strings.TrimSuffix(filePath, ext)
		outputFilePath := baseName + "-processed" + ext
		err = ioutil.WriteFile(outputFilePath, content, 0644)
		if err != nil {
			return nil, fmt.Errorf("error writing output file: %v", err)
		}
		return &processResult{outputPath: outputFilePath, imageCount: 0}, nil
	}

	// 从后向前处理，避免替换后的索引变化
	var newContent = make([]byte, len(content))
	copy(newContent, content)
	imageCount := 0

	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		altTextStart, altTextEnd := match[2], match[3]
		urlStart, urlEnd := match[4], match[5]

		imgAltText := string(content[altTextStart:altTextEnd])
		imgURL := string(content[urlStart:urlEnd])

		// 跳过已经是网络图片的引用
		if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
			continue
		}

		// 处理相对路径
		var imgPath string
		if filepath.IsAbs(imgURL) {
			imgPath = imgURL
		} else {
			imgPath = filepath.Join(filepath.Dir(filePath), imgURL)
		}

		// 检查文件是否存在
		if _, err := os.Stat(imgPath); os.IsNotExist(err) {
			log.Printf("Warning: Image file not found: %s\n", imgPath)
			continue
		}

		// 上传图片到 GitHub
		newURL, err := uploadImageToGitHub(imgPath, config)
		if err != nil {
			log.Printf("Warning: Failed to upload image %s: %v\n", imgPath, err)
			continue
		}

		// 替换图片引用
		newImgRef := fmt.Sprintf("![%s](%s)", imgAltText, newURL)
		oldImgRef := string(content[match[0]:match[1]])
		
		// 替换内容
		newContent = bytes.Replace(
			newContent,
			[]byte(oldImgRef),
			[]byte(newImgRef),
			1,
		)

		imageCount++
		log.Printf("Replaced: %s -> %s\n", imgURL, newURL)
	}

	// 生成新的输出文件名
	ext := filepath.Ext(filePath)
	baseName := strings.TrimSuffix(filePath, ext)
	outputFilePath := baseName + "-processed" + ext

	// 写入新文件
	err = ioutil.WriteFile(outputFilePath, newContent, 0644)
	if err != nil {
		return nil, fmt.Errorf("error writing output file: %v", err)
	}

	return &processResult{outputPath: outputFilePath, imageCount: imageCount}, nil
}

// 上传图片到 GitHub
func uploadImageToGitHub(imgPath string, config GitHubConfig) (string, error) {
	// 读取图片文件
	imgData, err := ioutil.ReadFile(imgPath)
	if err != nil {
		return "", fmt.Errorf("error reading image file: %v", err)
	}

	// 生成唯一的文件名
	imgExt := filepath.Ext(imgPath)
	timestamp := time.Now().UnixNano()
	imgName := fmt.Sprintf("%d%s", timestamp, imgExt)
	remotePath := filepath.Join(config.ImagesPath, imgName)

	// 首先检查仓库是否存在
	repoURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", config.Owner, config.Repo)
	req, err := http.NewRequest("GET", repoURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request to check repo: %v", err)
	}
	req.Header.Set("Authorization", "token "+config.Token)
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error checking repository: %v", err)
	}
	resp.Body.Close()
	
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("repository %s/%s not found - please check if it exists and is accessible with your token", 
			config.Owner, config.Repo)
	} else if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error accessing repository: %s", resp.Status)
	}
	
	// 尝试创建目录（通过创建一个.gitkeep文件）
	// 只有在第一次上传时尝试创建目录
	if !directoryChecked {
		err = ensureDirectoryExists(config)
		if err != nil {
			log.Printf("Warning: Could not ensure images directory exists: %v\n", err)
			// 继续执行，因为目录可能已经存在
		}
		directoryChecked = true
	}

	// 准备请求体
	base64Data := base64.StdEncoding.EncodeToString(imgData)
	requestBody := map[string]interface{}{
		"message": fmt.Sprintf("Upload image %s", imgName),
		"content": base64Data,
		"branch":  config.Branch,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	// 创建 API 请求
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", 
		config.Owner, config.Repo, remotePath)
	
	req, err = http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Authorization", "token "+config.Token)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error: %s - %s", resp.Status, string(body))
	}

	// 解析响应
	var uploadResp UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", fmt.Errorf("error parsing response: %v", err)
	}

	// 如果使用CDN，则返回jsDelivr链接
	if config.UseCDN {
		// 构建jsDelivr CDN URL
		// 格式: https://cdn.jsdelivr.net/gh/user/repo@branch/path/to/file
		cdnURL := fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s/%s@%s/%s",
			config.Owner, config.Repo, config.Branch, remotePath)
		return cdnURL, nil
	}

	return uploadResp.Content.DownloadURL, nil
}

// 确保目录存在
func ensureDirectoryExists(config GitHubConfig) error {
	// 创建一个.gitkeep文件来确保目录存在
	gitkeepPath := filepath.Join(config.ImagesPath, ".gitkeep")
	
	// 准备请求体
	emptyContent := ""
	base64Data := base64.StdEncoding.EncodeToString([]byte(emptyContent))
	requestBody := map[string]interface{}{
		"message": "Create images directory",
		"content": base64Data,
		"branch":  config.Branch,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	// 创建 API 请求
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", 
		config.Owner, config.Repo, gitkeepPath)
	
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Authorization", "token "+config.Token)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// 201 Created 或 422 Unprocessable Entity (如果文件已存在)都是可接受的
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != 422 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error: %s - %s", resp.Status, string(body))
	}

	return nil
}