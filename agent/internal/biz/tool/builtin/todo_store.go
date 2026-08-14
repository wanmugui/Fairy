package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LocalTodoItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority string `json:"priority"`
	Done     bool   `json:"done"`
	Result   string `json:"result,omitempty"`
}

type LocalTodoData struct {
	Date  string          `json:"date"`
	Items []LocalTodoItem `json:"items"`
}

type LocalTodoInput struct {
	ID          string
	Description string
}

type LocalTodoUpdate struct {
	ID          string
	Action      string
	Description string
	Result      string
}

type LocalTodoStore struct {
	workspace string
	now       func() time.Time
}

var localTodoFileLocks sync.Map

func NewLocalTodoStore(workspace string, clock func() time.Time) (*LocalTodoStore, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	if clock == nil {
		clock = time.Now
	}
	return &LocalTodoStore{workspace: filepath.Clean(absWorkspace), now: clock}, nil
}

func (s *LocalTodoStore) path(date string) string {
	return filepath.Join(s.workspace, "todos", date+".json")
}

func (s *LocalTodoStore) normalizedDate(date string) string {
	date = strings.TrimSpace(date)
	if date == "" || strings.EqualFold(date, "today") || date == "今天" {
		return s.now().Format("2006-01-02")
	}
	return date
}

func localTodoLock(path string) *sync.Mutex {
	lock := &sync.Mutex{}
	actual, _ := localTodoFileLocks.LoadOrStore(path, lock)
	return actual.(*sync.Mutex)
}

func (s *LocalTodoStore) Create(date string, inputs []LocalTodoInput) (LocalTodoData, error) {
	if len(inputs) == 0 {
		return LocalTodoData{}, fmt.Errorf("todo_list is empty")
	}
	date = s.normalizedDate(date)
	path := s.path(date)
	lock := localTodoLock(path)
	lock.Lock()
	defer lock.Unlock()
	items := make([]LocalTodoItem, 0, len(inputs))
	for index, input := range inputs {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = fmt.Sprintf("%d", index+1)
		}
		items = append(items, LocalTodoItem{ID: id, Title: input.Description, Priority: "medium"})
	}
	data := LocalTodoData{Date: date, Items: items}
	if err := writeJSONFileAtomically(path, data); err != nil {
		return LocalTodoData{}, err
	}
	return data, nil
}

func (s *LocalTodoStore) Append(date string, inputs []LocalTodoInput) (LocalTodoData, error) {
	if len(inputs) == 0 {
		return LocalTodoData{}, fmt.Errorf("todo_list is empty")
	}
	date = s.normalizedDate(date)
	path := s.path(date)
	lock := localTodoLock(path)
	lock.Lock()
	defer lock.Unlock()
	data, err := readLocalTodoData(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return LocalTodoData{}, err
		}
		data = LocalTodoData{Date: date, Items: make([]LocalTodoItem, 0)}
	}
	nextID := len(data.Items) + 1
	for _, input := range inputs {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = fmt.Sprintf("%d", nextID)
			nextID++
		}
		data.Items = append(data.Items, LocalTodoItem{ID: id, Title: input.Description, Priority: "medium"})
	}
	if err := writeJSONFileAtomically(path, data); err != nil {
		return LocalTodoData{}, err
	}
	return data, nil
}

func (s *LocalTodoStore) Update(date string, updates []LocalTodoUpdate) (LocalTodoData, error) {
	if len(updates) == 0 {
		return LocalTodoData{}, fmt.Errorf("updates is empty")
	}
	date = s.normalizedDate(date)
	path := s.path(date)
	lock := localTodoLock(path)
	lock.Lock()
	defer lock.Unlock()
	data, err := readLocalTodoData(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LocalTodoData{}, fmt.Errorf("no todolist file found for %s", date)
		}
		return LocalTodoData{}, err
	}
	for _, update := range updates {
		id := strings.TrimSpace(update.ID)
		if id == "" {
			continue
		}
		for index := range data.Items {
			if data.Items[index].ID != id {
				continue
			}
			switch update.Action {
			case "edit":
				data.Items[index].Title = update.Description
				data.Items[index].Done = false
				data.Items[index].Result = ""
			case "finish":
				data.Items[index].Done = true
				data.Items[index].Result = update.Result
			}
			break
		}
	}
	if err := writeJSONFileAtomically(path, data); err != nil {
		return LocalTodoData{}, err
	}
	return data, nil
}

func (s *LocalTodoStore) Read(date string) (LocalTodoData, error) {
	date = s.normalizedDate(date)
	path := s.path(date)
	lock := localTodoLock(path)
	lock.Lock()
	defer lock.Unlock()
	return readLocalTodoData(path)
}

func readLocalTodoData(path string) (LocalTodoData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LocalTodoData{}, err
	}
	var data LocalTodoData
	if err := json.Unmarshal(raw, &data); err != nil {
		return LocalTodoData{}, fmt.Errorf("parse todo file: %w", err)
	}
	if data.Items == nil {
		data.Items = make([]LocalTodoItem, 0)
	}
	return data, nil
}
