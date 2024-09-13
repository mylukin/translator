package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
	"github.com/urfave/cli/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

type OrderedMap struct {
	keys   []string
	values map[string]string
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		keys:   make([]string, 0),
		values: make(map[string]string),
	}
}

func (om *OrderedMap) Set(key, value string) {
	if _, exists := om.values[key]; !exists {
		om.keys = append(om.keys, key)
	}
	om.values[key] = value
}

func (om *OrderedMap) Get(key string) (string, bool) {
	value, exists := om.values[key]
	return value, exists
}

type debugTransport struct {
	Transport http.RoundTripper
}

func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic in DumpRequestOut: %v\n", r)
		}
	}()

	// Create the client trace
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			fmt.Printf("Got Conn: %+v\n", info)
		},
		DNSStart: func(info httptrace.DNSStartInfo) {
			fmt.Printf("DNS Start: %+v\n", info)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			fmt.Printf("DNS Done: %+v\n", info)
		},
		ConnectStart: func(network, addr string) {
			fmt.Printf("Connect Start: %s %s\n", network, addr)
		},
		ConnectDone: func(network, addr string, err error) {
			fmt.Printf("Connect Done: %s %s %v\n", network, addr, err)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			fmt.Printf("Wrote Request: %+v\n", info)
		},
	}

	// Dump the request for debugging purposes
	dump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		fmt.Printf("Failed to dump request: %v\n", err)
	} else {
		fmt.Printf("Request: %s\n", dump)
	}

	// Add the trace to the request
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Execute the request
	resp, err := d.Transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Dump the response for debugging purposes
	dump, err = httputil.DumpResponse(resp, true)
	if err != nil {
		fmt.Printf("Failed to dump response: %v\n", err)
	} else {
		fmt.Printf("Response: %s\n", dump)
	}

	return resp, nil
}

// 定义版本号
const Version = "0.1.7"
const newlinePlaceholder = "{{NEWLINE_PLACEHOLDER}}"

func main() {
	app := &cli.App{
		Name:    "translator",
		Usage:   "Translate JSON file values using OpenAI API",
		Version: Version, // 添加版本号
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "input",
				Aliases:  []string{"i"},
				Usage:    "Input JSON file path",
				Value:    "locales/en.json",
				Required: false,
			},
			&cli.StringFlag{
				Name:     "language",
				Aliases:  []string{"l"},
				Usage:    "Target language code for translation (e.g., zh, es, fr)",
				Required: true,
			},
			&cli.IntFlag{
				Name:     "batchSize",
				Aliases:  []string{"b"},
				Usage:    "Number of texts to translate in each batch",
				Value:    100,
				Required: false,
			},
			&cli.StringFlag{
				Name:     "env",
				Aliases:  []string{"e"},
				Usage:    "Path to .env file",
				Value:    ".env",
				Required: false,
			},
			&cli.StringFlag{
				Name:     "output",
				Aliases:  []string{"o"},
				Usage:    "Output directory for translated files (default: same as input file)",
				Required: false,
			},
		},
		Action: translateJSON,
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
func translateJSON(c *cli.Context) error {
	inputFile := c.String("input")
	languageCode := c.String("language")
	batchSize := c.Int("batchSize")
	envFile := c.String("env")
	outputDir := c.String("output")

	// 如果没有指定输出目录，使用输入文件的目录
	if outputDir == "" {
		outputDir = filepath.Dir(inputFile)
	}

	outputFile := filepath.Join(outputDir, fmt.Sprintf("%s.json", languageCode))

	err := godotenv.Load(envFile)
	if err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY not found in .env file")
	}

	// 读取自定义 prompt
	customPrompt := os.Getenv("CUSTOM_PROMPT")

	config := openai.DefaultConfig(apiKey)
	apiEndpoint := os.Getenv("OPENAI_API_ENDPOINT")
	if apiEndpoint != "" {
		config.BaseURL = apiEndpoint
	}
	config.HTTPClient = &http.Client{
		Transport: &debugTransport{http.DefaultTransport},
	}
	client := openai.NewClientWithConfig(config)

	inputJSON, err := readJSONFile(inputFile)
	if err != nil {
		return fmt.Errorf("error reading input file: %v", err)
	}

	outputJSON, err := readJSONFile(outputFile)
	if err != nil {
		return fmt.Errorf("error reading output file: %v", err)
	}

	mergedJSON, untranslatedKeys := mergeJSON(inputJSON, outputJSON)

	targetLanguage := Code2Lang(languageCode)

	if len(untranslatedKeys) > 0 {
		toTranslate := NewOrderedMap()
		for _, key := range untranslatedKeys {
			if value, exists := mergedJSON.Get(key); exists {
				toTranslate.Set(key, value)
			}
		}

		translatedData, err := translateJSONValues(client, toTranslate, targetLanguage, batchSize, customPrompt)
		if err != nil {
			return fmt.Errorf("error translating JSON values: %v", err)
		}

		for _, key := range translatedData.keys {
			if value, exists := translatedData.Get(key); exists {
				mergedJSON.Set(key, value)
			}
		}
	}

	err = writeJSONFile(outputFile, mergedJSON)
	if err != nil {
		return fmt.Errorf("error writing output file: %v", err)
	}

	fmt.Printf("Translation complete. Output saved to %s\n", outputFile)
	return nil
}

func readJSONFile(filename string) (*OrderedMap, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return NewOrderedMap(), nil
		}
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	_, err = decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("error reading JSON start: %v", err)
	}

	orderedMap := NewOrderedMap()

	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("error reading JSON key: %v", err)
		}

		var value string
		err = decoder.Decode(&value)
		if err != nil {
			return nil, fmt.Errorf("error reading JSON value: %v", err)
		}

		orderedMap.Set(key.(string), value)
	}

	_, err = decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("error reading JSON end: %v", err)
	}

	return orderedMap, nil
}

func mergeJSON(input, output *OrderedMap) (*OrderedMap, []string) {
	merged := NewOrderedMap()
	var untranslatedKeys []string

	for _, key := range input.keys {
		inputValue, _ := input.Get(key)
		merged.Set(key, inputValue)

		if outputValue, exists := output.Get(key); !exists || outputValue == inputValue {
			untranslatedKeys = append(untranslatedKeys, key)
		} else {
			merged.Set(key, outputValue)
		}
	}

	return merged, untranslatedKeys
}

func writeJSONFile(filename string, data *OrderedMap) error {
	err := os.MkdirAll(filepath.Dir(filename), 0755)
	if err != nil {
		return fmt.Errorf("error creating output directory: %v", err)
	}

	var buf bytes.Buffer
	buf.WriteString("{\n")

	for i, key := range data.keys {
		value, _ := data.Get(key)

		// 编码键
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return fmt.Errorf("error encoding key: %v", err)
		}

		// 编码值
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("error encoding value: %v", err)
		}

		// 写入键值对
		buf.WriteString(fmt.Sprintf("  %s: %s", keyJSON, valueJSON))

		// 如果不是最后一个元素，添加逗号
		if i < len(data.keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}

	buf.WriteString("}\n")

	// 写入文件
	err = os.WriteFile(filename, buf.Bytes(), 0644)
	if err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	return nil
}

func translateJSONValues(client *openai.Client, data *OrderedMap, targetLanguage string, batchSize int, customPrompt string) (*OrderedMap, error) {
	translatedData := NewOrderedMap()
	batch := make([]string, 0, batchSize)
	batchKeys := make([]string, 0, batchSize)

	for _, key := range data.keys {
		value, _ := data.Get(key)
		value = strings.ReplaceAll(value, "\n", newlinePlaceholder)
		batch = append(batch, value)
		batchKeys = append(batchKeys, key)

		if len(batch) == batchSize {
			translatedBatch, err := translateText(client, batch, targetLanguage, customPrompt)
			if err != nil {
				return nil, fmt.Errorf("error translating batch: %v", err)
			}
			for i, translatedValue := range translatedBatch {
				translatedValue = strings.ReplaceAll(translatedValue, newlinePlaceholder, "\n")
				translatedData.Set(batchKeys[i], translatedValue)
			}
			batch = batch[:0]
			batchKeys = batchKeys[:0]
		}
	}

	// 处理剩余的不足一个批次的项目
	if len(batch) > 0 {
		translatedBatch, err := translateText(client, batch, targetLanguage, customPrompt)
		if err != nil {
			return nil, fmt.Errorf("error translating final batch: %v", err)
		}
		for i, translatedValue := range translatedBatch {
			translatedValue = strings.ReplaceAll(translatedValue, newlinePlaceholder, "\n")
			translatedData.Set(batchKeys[i], translatedValue)
		}
	}

	return translatedData, nil
}

func translateText(client *openai.Client, texts []string, targetLanguage string, customPrompt string) ([]string, error) {
	systemPrompt := fmt.Sprintf("You are a professional translator specializing in localizing web content. Your task is to translate the given texts accurately while preserving all HTML structure and the special placeholder {{NEWLINE_PLACEHOLDER}}. Strictly maintain all HTML tags and the placeholder in their original form and position. Translate only the content between tags, not the tags themselves or the placeholder. Provide only the translated texts, each on a new line, maintaining the original order. Do not add any comments, explanations, or additional formatting.")

	if customPrompt != "" {
		systemPrompt += " " + customPrompt
	}

	prompt := fmt.Sprintf("Translate the following %d texts to %s. Maintain the original order and preserve all HTML tags and the placeholder {{NEWLINE_PLACEHOLDER}} exactly as they appear. Do not translate the content inside HTML tags or the placeholder. Return each translated text on a new line, without any explanations, quotation marks, line numbers, or additional formatting:\n\n%s", len(texts), targetLanguage, strings.Join(texts, "\n"))

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)

	if err != nil {
		return nil, err
	}

	translatedTexts := strings.Split(resp.Choices[0].Message.Content, "\n")

	// 确保翻译后的文本数量与原文本数量相同
	if len(translatedTexts) != len(texts) {
		return nil, fmt.Errorf("translation mismatch: got %d translations for %d texts", len(translatedTexts), len(texts))
	}

	// 清理翻译后的文本
	for i, text := range translatedTexts {
		translatedTexts[i] = cleanTranslation(text)
	}

	return translatedTexts, nil
}

func cleanTranslation(translation string) string {
	// 只移除首尾的空白字符
	return strings.TrimSpace(translation)
}

func Code2Lang(code string) string {
	tag := language.Make(code)
	return display.English.Languages().Name(tag)
}
