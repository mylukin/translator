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

type debugTransport struct {
	Transport http.RoundTripper
}

func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func main() {
	app := &cli.App{
		Name:  "json-translator",
		Usage: "Translate JSON file values using OpenAI API",
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
				Value:    255,
				Required: false,
			},
			&cli.StringFlag{
				Name:     "env",
				Aliases:  []string{"e"},
				Usage:    "Path to .env file",
				Value:    ".env",
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

	// 设置输出文件路径
	outputFile := filepath.Join("locales", fmt.Sprintf("%s.json", languageCode))

	// 加载 .env 文件
	err := godotenv.Load(envFile)
	if err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	// 从 .env 文件读取 API 密钥
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY not found in .env file")
	}

	// 创建 OpenAI 客户端
	config := openai.DefaultConfig(apiKey)
	apiEndpoint := os.Getenv("OPENAI_API_ENDPOINT")
	if apiEndpoint != "" {
		config.BaseURL = apiEndpoint
	}
	config.HTTPClient = &http.Client{
		Transport: &debugTransport{http.DefaultTransport},
	}
	client := openai.NewClientWithConfig(config)

	// 读取输入 JSON 文件
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("error reading input file: %v", err)
	}

	// 解析 JSON 数据
	var jsonData map[string]string
	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %v", err)
	}

	// 获取目标语言的名称
	targetLanguage := Code2Lang(languageCode)

	// 翻译 JSON 值
	translatedData, err := translateJSONValues(client, jsonData, targetLanguage, batchSize)
	if err != nil {
		return fmt.Errorf("error translating JSON values: %v", err)
	}

	// 将翻译后的 JSON 写入新文件
	// 使用自定义的 JSON 编码器
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(translatedData)
	if err != nil {
		return fmt.Errorf("error encoding translated JSON: %v", err)
	}

	// 确保输出目录存在
	err = os.MkdirAll(filepath.Dir(outputFile), 0755)
	if err != nil {
		return fmt.Errorf("error creating output directory: %v", err)
	}

	// 将编码后的 JSON 写入文件
	err = os.WriteFile(outputFile, buf.Bytes(), 0644)
	if err != nil {
		return fmt.Errorf("error writing output file: %v", err)
	}

	fmt.Printf("Translation complete. Output saved to %s\n", outputFile)
	return nil
}

func translateJSONValues(client *openai.Client, data map[string]string, targetLanguage string, batchSize int) (map[string]string, error) {
	translatedData := make(map[string]string)
	keys := make([]string, 0, len(data))
	values := make([]string, 0, len(data))

	for key, value := range data {
		keys = append(keys, key)
		values = append(values, value)

		if len(values) == batchSize {
			translatedBatch, err := translateText(client, values, targetLanguage)
			if err != nil {
				return nil, fmt.Errorf("error translating batch: %v", err)
			}
			for i, translatedValue := range translatedBatch {
				translatedData[keys[i]] = translatedValue
			}
			keys = keys[:0]
			values = values[:0]
		}
	}

	// Translate any remaining items
	if len(values) > 0 {
		translatedBatch, err := translateText(client, values, targetLanguage)
		if err != nil {
			return nil, fmt.Errorf("error translating final batch: %v", err)
		}
		for i, translatedValue := range translatedBatch {
			translatedData[keys[i]] = translatedValue
		}
	}

	return translatedData, nil
}

func translateText(client *openai.Client, texts []string, targetLanguage string) ([]string, error) {
	prompt := fmt.Sprintf("Translate the following %d texts to %s. It is crucial to maintain the original order and preserve all HTML tags exactly as they appear. Do not translate the content inside HTML tags. Return each translated text on a new line, without any explanations, quotation marks, line numbers, or additional formatting:\n\n%s", len(texts), targetLanguage, strings.Join(texts, "\n"))

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a professional translator specializing in localizing web content. Your task is to translate the given texts accurately while preserving all HTML structure. Strictly maintain all HTML tags in their original form and position. Translate only the content between tags, not the tags themselves. Provide only the translated texts, each on a new line, maintaining the original order. Do not add any comments, explanations, or additional formatting.",
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
