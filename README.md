# JSON Translator

JSON Translator is a command-line tool that translates JSON file values using the OpenAI API. It's designed to help with localization tasks by automatically translating JSON language files while preserving the structure and special characters.

## Features

- Translates JSON files using OpenAI's powerful language models
- Preserves HTML tags and emoji in the translated text
- Supports batch translation for improved efficiency
- Customizable batch size for translation requests
- Supports various target languages
- Debug mode for API request and response inspection

## Installation

1. Ensure you have Go installed on your system.
2. Install the JSON Translator tool:
   ```
   go install github.com/mylukin/translator@latest
   ```

## Configuration

1. Create a `.env` file in the directory where you'll run the translator.
2. Add your OpenAI API key to the `.env` file:
   ```
   OPENAI_API_KEY=your_api_key_here
   ```
3. (Optional) If you're using a different API endpoint, you can specify it in the `.env` file:
   ```
   OPENAI_API_ENDPOINT=https://your-api-endpoint.com
   ```

## Usage

After installation, you can run the translator with the following command:

```
translator --input path/to/input.json --language target_language_code
```

### Command-line Options

- `--input`, `-i`: Input JSON file path (default: "locales/en.json")
- `--language`, `-l`: Target language code for translation (e.g., zh, es, fr) (required)
- `--batchSize`, `-b`: Number of texts to translate in each batch (default: 255)
- `--env`, `-e`: Path to .env file (default: ".env")

Example:

```
translator -i locales/en.json -l zh -b 100
```

This command will translate the English JSON file to Chinese, using a batch size of 100 for API requests.

## Development

If you want to contribute or modify the translator:

1. Clone the repository:
   ```
   git clone https://github.com/mylukin/translator.git
   ```
2. Navigate to the project directory:
   ```
   cd translator
   ```
3. Install the required dependencies:
   ```
   go mod tidy
   ```
4. Make your changes and test them:
   ```
   go run . -i path/to/input.json -l target_language_code
   ```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[MIT License](LICENSE)

## Disclaimer

This tool uses the OpenAI API, which may incur costs. Please be aware of your usage and any associated fees.