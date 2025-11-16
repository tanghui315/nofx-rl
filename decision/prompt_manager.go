package decision

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PromptTemplate 系统提示词模板
type PromptTemplate struct {
	Name    string // 模板名称（文件名，不含扩展名）
	Content string // 模板内容
}

// PromptManager 提示词管理器
type PromptManager struct {
	templates map[string]*PromptTemplate
	mu        sync.RWMutex
}

var (
	// globalPromptManager 全局提示词管理器
	globalPromptManager *PromptManager
	// promptsDir 提示词文件夹路径
	promptsDir = "prompts"
)

// init 包初始化时加载所有提示词模板
func init() {
	globalPromptManager = NewPromptManager()
	if err := globalPromptManager.LoadTemplates(promptsDir); err != nil {
		log.Printf("⚠️  加载提示词模板失败: %v", err)
	} else {
		log.Printf("✓ 已加载 %d 个系统提示词模板", len(globalPromptManager.templates))
	}
}

// NewPromptManager 创建提示词管理器
func NewPromptManager() *PromptManager {
	return &PromptManager{
		templates: make(map[string]*PromptTemplate),
	}
}

// LoadTemplates 从指定目录（递归）加载所有 .txt 模板
func (pm *PromptManager) LoadTemplates(dir string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("提示词目录不存在: %s", dir)
	}

	var count int
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".txt" {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			log.Printf("⚠️  读取提示词文件失败 %s: %v", path, rerr)
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		key := strings.TrimSuffix(rel, filepath.Ext(rel)) // 子路径作为键，如 pullback_ema/prompt_pullback_ema
		key = filepath.ToSlash(key)
		pm.templates[key] = &PromptTemplate{Name: key, Content: string(content)}
		count++
		log.Printf("  📄 加载提示词模板: %s (%s)", key, rel)
		return nil
	})
	if err != nil {
		return fmt.Errorf("扫描提示词目录失败: %w", err)
	}
	if count == 0 {
		log.Printf("⚠️  提示词目录 %s 中没有找到 .txt 文件", dir)
	}
	return nil
}

// GetTemplate 获取指定名称的提示词模板
func (pm *PromptManager) GetTemplate(name string) (*PromptTemplate, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	template, exists := pm.templates[name]
	if !exists {
		return nil, fmt.Errorf("提示词模板不存在: %s", name)
	}

	return template, nil
}

// GetAllTemplateNames 获取所有模板名称列表
func (pm *PromptManager) GetAllTemplateNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.templates))
	for name := range pm.templates {
		names = append(names, name)
	}

	return names
}

// GetAllTemplates 获取所有模板
func (pm *PromptManager) GetAllTemplates() []*PromptTemplate {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	templates := make([]*PromptTemplate, 0, len(pm.templates))
	for _, template := range pm.templates {
		templates = append(templates, template)
	}

	return templates
}

// ReloadTemplates 重新加载所有模板
func (pm *PromptManager) ReloadTemplates(dir string) error {
	pm.mu.Lock()
	pm.templates = make(map[string]*PromptTemplate)
	pm.mu.Unlock()

	return pm.LoadTemplates(dir)
}

// === 全局函数（供外部调用）===

// GetPromptTemplate 获取指定名称的提示词模板（全局函数）
func GetPromptTemplate(name string) (*PromptTemplate, error) {
	return globalPromptManager.GetTemplate(name)
}

// GetAllPromptTemplateNames 获取所有模板名称（全局函数）
func GetAllPromptTemplateNames() []string {
	return globalPromptManager.GetAllTemplateNames()
}

// GetAllPromptTemplates 获取所有模板（全局函数）
func GetAllPromptTemplates() []*PromptTemplate {
	return globalPromptManager.GetAllTemplates()
}

// ReloadPromptTemplates 重新加载所有模板（全局函数）
func ReloadPromptTemplates() error {
	return globalPromptManager.ReloadTemplates(promptsDir)
}
