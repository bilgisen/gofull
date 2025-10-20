// ===========================================
// README.md
// ===========================================

# 🚀 RSS Full-Text Proxy

Transform any RSS feed into a full-text version. Perfect for RSS readers that only show summaries.

## ✨ Features

- 🔍 Automatic full-text extraction from articles
- ⚡ Smart caching for better performance
- 🎨 Clean, readable output
- 🌐 Works with any RSS feed
- 🆓 Free and open source

## 🚀 Quick Start

### Local Development

bash
# Clone the repository
git clone https://github.com/bilgisen/gofull.git
cd gofull

# Install dependencies
go mod download

# Run the server
go run main.go


Visit http://localhost:8080

### Docker

bash
docker build -t rss-proxy .
docker run -p 8080:8080 rss-proxy


## 📖 Usage

### API Endpoint

GET /feed?url={RSS_FEED_URL}&limit={NUMBER}


### Parameters

- url (required): The RSS feed URL to convert
- limit (optional): Number of articles to process (default: 10, max: 50)

### Example

bash
curl "http://localhost:8080/feed?url=https://techcrunch.com/feed/&limit=5"


## 🌐 Deploy

### Railway

[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new)

### Render

1. Fork this repo
2. Create new Web Service on Render
3. Connect your repo
4. Deploy!

### Fly.io

bash
flyctl launch
flyctl deploy


## 🛠️ Tech Stack

- Go 1.21+
- go-readability for content extraction
- gofeed for RSS parsing
- gorilla/feeds for RSS generation

## 📝 License

MIT

## 🤝 Contributing

Contributions welcome! Please open an issue or PR.