package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MaxActiveTasks  = 3
	MaxFilesPerTask = 3
	MkDirPerm       = 0755
	TaskIDPathParts = 3
)

// Config app configs.
type Config struct {
	AllowedExtensions map[string]bool
	MaxFilesPerTask   int
	MaxActiveTasks    int
	TempFolder        string
	ServerPort        string
}

var (
	config = Config{
		AllowedExtensions: map[string]bool{
			".pdf":  true,
			".jpeg": true,
			".jpg":  true,
		},
		MaxFilesPerTask: MaxFilesPerTask,
		MaxActiveTasks:  MaxActiveTasks,
		TempFolder:      "temp_archives",
		ServerPort:      "8080",
	}

	tasks      = make(map[string]*Task)
	tasksMutex sync.Mutex
	taskSem    chan struct{}
)

// Task create archives.
type Task struct {
	ID        string
	Status    string
	Files     []File
	Errors    []string
	ZipPath   string
	CreatedAt time.Time
	Mutex     sync.Mutex
}

// File download files.
type File struct {
	URL      string
	Filename string
	Data     []byte
}

func init() {
	taskSem = make(chan struct{}, config.MaxActiveTasks)

	if err := os.MkdirAll(config.TempFolder, MkDirPerm); err != nil {
		panic(fmt.Sprintf("Failed to create temp folder: %v", err))
	}
}

func main() {
	http.HandleFunc("/tasks", handleTasks)
	http.HandleFunc("/tasks/", handleTaskFiles)

	fmt.Printf("Server starting on port %s...\n", config.ServerPort)
	if err := http.ListenAndServe(":"+config.ServerPort, nil); err != nil {
		panic(fmt.Sprintf("Failed to start server: %v", err))
	}
}

// generateTaskID generates ID for task.
func generateTaskID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// isValidURL checks is URL valid.
func isValidURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// handleTasks create new task.
func handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	select {
	case taskSem <- struct{}{}:
		taskID := generateTaskID()
		newTask := &Task{
			ID:        taskID,
			Status:    "created",
			CreatedAt: time.Now(),
		}

		tasksMutex.Lock()
		tasks[taskID] = newTask
		tasksMutex.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, err := fmt.Fprintf(w, `{"task_id": "%s", "status": "created"}`, taskID)
		if err != nil {
			fmt.Println("Failed to provide info for user: ", err)

			return
		}
	default:
		http.Error(w, `{"error": "Server is busy. Try again later."}`, http.StatusServiceUnavailable)
	}
}

// handleTaskFiles add files and check status.
func handleTaskFiles(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < TaskIDPathParts {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)

		return
	}

	taskID := pathParts[2]
	tasksMutex.Lock()
	task, exists := tasks[taskID]
	tasksMutex.Unlock()

	if !exists {
		http.Error(w, "Task not found", http.StatusNotFound)

		return
	}

	switch r.Method {
	case http.MethodPost:
		task.Mutex.Lock()
		defer task.Mutex.Unlock()

		if len(task.Files) >= config.MaxFilesPerTask {
			http.Error(w, `{"error": "Maximum files per task reached"}`, http.StatusBadRequest)

			return
		}

		var request struct {
			URL string `json:"url"`
		}

		if err := decodeJSONBody(w, r, &request); err != nil {
			http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)

			return
		}

		if !isValidURL(request.URL) {
			http.Error(w, `{"error": "Invalid URL"}`, http.StatusBadRequest)

			return
		}

		if !hasAllowedExtension(request.URL) {
			http.Error(w, `{"error": "File type not allowed"}`, http.StatusBadRequest)

			return
		}

		data, filename, err := downloadFile(request.URL)
		if err != nil {
			task.Errors = append(task.Errors, fmt.Sprintf("Failed to download %s: %v", request.URL, err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, err := fmt.Fprintf(w, `{"task_id": "%s", "status": "%s", "error": "Failed to download file, please, try other service"}`, task.ID, task.Status)
			if err != nil {
				fmt.Println("Failed to provide info for user: ", err)

				return
			}

			return
		}

		task.Files = append(task.Files, File{
			URL:      request.URL,
			Filename: filename,
			Data:     data,
		})

		if len(task.Files) == config.MaxFilesPerTask {
			task.Status = "processing"
			go createZipArchive(task)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, err = fmt.Fprintf(w, `{"task_id": "%s", "status": "%s", "files_count": %d}`, task.ID, task.Status, len(task.Files))
		if err != nil {
			fmt.Println("Failed to provide info to user: ", err)

			return
		}

	case http.MethodGet:
		task.Mutex.Lock()
		defer task.Mutex.Unlock()

		response := map[string]interface{}{
			"task_id":     task.ID,
			"status":      task.Status,
			"files_count": len(task.Files),
			"errors":      task.Errors,
		}

		if task.Status == "completed" && task.ZipPath != "" {
			response["download_url"] = fmt.Sprintf("/download/%s", filepath.Base(task.ZipPath))
		}

		sendJSONResponse(w, http.StatusOK, response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// createZipArchive create ZIP archive.
func createZipArchive(task *Task) {
	defer func() {
		<-taskSem
	}()

	zipPath := filepath.Join(config.TempFolder, fmt.Sprintf("%s.zip", task.ID))
	zipFile, err := os.Create(zipPath)
	if err != nil {
		task.Mutex.Lock()
		task.Status = "failed"
		task.Errors = append(task.Errors, fmt.Sprintf("Failed to create zip file: %v", err))
		task.Mutex.Unlock()

		return
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for _, file := range task.Files {
		fileWriter, err := zipWriter.Create(file.Filename)
		if err != nil {
			task.Mutex.Lock()
			task.Errors = append(task.Errors, fmt.Sprintf("Failed to add file %s to archive: %v", file.Filename, err))
			task.Mutex.Unlock()
			continue
		}

		if _, err := fileWriter.Write(file.Data); err != nil {
			task.Mutex.Lock()
			task.Errors = append(task.Errors, fmt.Sprintf("Failed to write file %s to archive: %v", file.Filename, err))
			task.Mutex.Unlock()
		}
	}

	task.Mutex.Lock()
	task.Status = "completed"
	task.ZipPath = zipPath
	task.Mutex.Unlock()
}

// parseURL decodes url and extract file name
func parseURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	q := u.Query()
	if filename := q.Get("filename"); filename != "" {
		return filename, nil
	}

	path := u.Path
	if path == "" {
		return "", fmt.Errorf("no filename in URL")
	}

	return filepath.Base(path), nil
}

// hasAllowedExtension checks is file extension provided is correct.
func hasAllowedExtension(rawURL string) bool {
	filename, err := parseURL(rawURL)
	if err != nil {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".jpeg" || ext == ".jpg" || ext == ".pdf" {
		return true
	}

	return false
}

// downloadFile downloads file.
func downloadFile(url string) ([]byte, string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	filename := filepath.Base(url)
	if filename == "." || filename == "/" {
		filename = "file" + filepath.Ext(url)
	}

	return data, filename, nil
}

// decodeJSONBody decodes provided json body.
func decodeJSONBody(_ http.ResponseWriter, r *http.Request, dst interface{}) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("content type is not application/json")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return err
	}

	return nil
}

// sendJSONResponse sends JSON response.
func sendJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		fmt.Println("Failed to encode response into JSON: ", err)

		return
	}
}
