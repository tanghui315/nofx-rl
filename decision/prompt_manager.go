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
	templates         map[string]*PromptTemplate
	baseTemplates     map[string]*PromptTemplate // 基础模板
	strategyTemplates map[string]*PromptTemplate // 策略模板（片段）
	mu                sync.RWMutex
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
		log.Printf("✓ 已加载 %d 个系统提示词模板 (含 %d 个策略, %d 个基础)", 
			len(globalPromptManager.templates), 
			len(globalPromptManager.strategyTemplates),
			len(globalPromptManager.baseTemplates))
	}
}

// NewPromptManager 创建提示词管理器
func NewPromptManager() *PromptManager {
	return &PromptManager{
		templates:         make(map[string]*PromptTemplate),
		baseTemplates:     make(map[string]*PromptTemplate),
		strategyTemplates: make(map[string]*PromptTemplate),
	}
}

// LoadTemplates 从指定目录加载所有提示词模板
func (pm *PromptManager) LoadTemplates(dir string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// 检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("提示词目录不存在: %s", dir)
	}

	// 1. 加载基础模板 (base/*.txt)
	baseDir := filepath.Join(dir, "base")
	if err := pm.loadSubDir(baseDir, pm.baseTemplates, "基础"); err != nil {
		log.Printf("⚠️  加载基础模板失败: %v", err)
	}

	// 2. 加载策略模板 (strategies/*.txt)
	stratDir := filepath.Join(dir, "strategies")
	if err := pm.loadSubDir(stratDir, pm.strategyTemplates, "策略"); err != nil {
		log.Printf("⚠️  加载策略模板失败: %v", err)
	}

	// 3. 加载根目录模板 (兼容旧模式，作为完整模板)
	// 扫描目录中的所有 .txt 文件 (不递归)
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(file), ".txt")
		pm.templates[name] = &PromptTemplate{Name: name, Content: string(content)}
		log.Printf("  📄 加载根目录模板: %s", name)
	}

	// 4. 自动拼装策略模板 (Base + Strategy) -> Main Templates
	// 默认使用 common_rules 作为基底
	commonBase, hasCommon := pm.baseTemplates["common_rules"]
	
	for name, strat := range pm.strategyTemplates {
		if hasCommon {
			// 拼接：Strategy + Base (或 Base + Strategy，取决于偏好，通常 Base 放后面或前面)
			// 之前的单体文件是 Strategy 在前，Base 在后？
			// adaptive_moderate_hist_v6_3.txt 中 Base (基础交易约束) 在前面还是后面？
			// 原文中 "基础交易约束" 在 Line 35，"决策流程" 在 Line 62。
			// 所以是 Base -> Strategy。
			// 但 common_rules.txt 是提取自 Line 35。 Line 1-34 是 Intro。
			// 为了保持一致性，我们可以：Base + Strategy。
			// 或者 Strategy Header + Base + Strategy Body。
			// 简单起见：Base + \n\n + Strategy
			
			fullContent := commonBase.Content + "\n\n" + strat.Content
			pm.templates[name] = &PromptTemplate{
				Name:    name,
				Content: fullContent,
			}
			log.Printf("  🧩 自动拼装策略: %s (Common + Strategy)", name)
		} else {
			// 如果没有 common_rules，直接使用策略片段（可能不完整，但作为回退）
			pm.templates[name] = strat
			log.Printf("  ⚠️ 未找到 common_rules，仅加载策略片段: %s", name)
		}
	}

	return nil
}

// loadSubDir 加载子目录模板
func (pm *PromptManager) loadSubDir(dir string, targetMap map[string]*PromptTemplate, typeName string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil // 目录不存在忽略
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return err
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(file), ".txt")
		targetMap[name] = &PromptTemplate{Name: name, Content: string(content)}
		log.Printf("  📄 加载%s模板: %s", typeName, name)
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
