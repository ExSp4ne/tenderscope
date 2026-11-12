package eis

import (
	"archive/zip"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const TmpDir = "tmp"

// TenderMeta - метаданные закупки (поля совпадают с docs/contracts.md)
type TenderMeta struct {
	Title    string  `json:"title"`
	Customer string  `json:"customer"`
	Price    float64 `json:"price"`
	Law      string  `json:"law"` // "44-FZ" или "223-FZ"
}

var (
	re44     = regexp.MustCompile(`^\d{19}$`)
	re223    = regexp.MustCompile(`^\d{11}$`)
	reTags   = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces = regexp.MustCompile(`\s+`)
	reLink   = regexp.MustCompile(`(?s)<a[^>]+href="([^"]*(?:filestore|download)[^"]*)"[^>]*>(.*?)</a>`)
	client   = newClient()
)

type fileLink struct{ url, text string }

// DownloadTenderFiles получает метаданные закупки и скачивает файл проекта контракта в backend/tmp/
func DownloadTenderFiles(tenderNumber string) (filePath string, meta TenderMeta, err error) {
	var infoURL, docsURL string
	switch {
	case re44.MatchString(tenderNumber):
		meta.Law = "44-FZ"
		infoURL = "https://zakupki.gov.ru/epz/order/notice/ea44/view/common-info.html?regNumber=" + tenderNumber
		docsURL = "https://zakupki.gov.ru/epz/order/notice/ea44/view/documents.html?regNumber=" + tenderNumber
	case re223.MatchString(tenderNumber):
		meta.Law = "223-FZ"
		infoURL = "https://zakupki.gov.ru/223/purchase/public/purchase/info/common-info.html?regNumber=" + tenderNumber
		docsURL = "https://zakupki.gov.ru/223/purchase/public/purchase/info/documents.html?regNumber=" + tenderNumber
	default:
		return "", meta, fmt.Errorf("неверный номер закупки: нужно 19 цифр (44-ФЗ) или 11 цифр (223-ФЗ)")
	}

	// Метаданные
	page, err := getPage(infoURL)
	if err != nil {
		return "", meta, err
	}
	meta.Title = field(page, "Объект закупки", "Наименование закупки")
	meta.Customer = field(page, "Заказчик", "Наименование заказчика")
	meta.Price = parsePrice(field(page, "Начальная цена контракта", "Начальная (максимальная) цена договора", "Начальная (максимальная) цена контракта"))
	if meta.Title == "" {
		return "", meta, fmt.Errorf("не удалось разобрать страницу закупки %s (изменилась вёрстка ЕИС?)", tenderNumber)
	}

	// Ссылки на документы
	docs, err := getPage(docsURL)
	if err != nil {
		return "", meta, err
	}
	base, _ := url.Parse(docsURL)
	seen := map[string]bool{}
	var candidates []fileLink
	for _, m := range reLink.FindAllStringSubmatch(docs, -1) {
		href, _ := url.Parse(html.UnescapeString(m[1]))
		full := base.ResolveReference(href).String()
		text := strings.TrimSpace(reSpaces.ReplaceAllString(html.UnescapeString(reTags.ReplaceAllString(m[2], " ")), " "))
		t := strings.ToLower(text)
		isContract := strings.Contains(t, "контракт") || strings.Contains(t, "договор")
		if seen[full] || !(isContract || strings.HasSuffix(t, ".zip")) {
			continue
		}
		seen[full] = true
		candidates = append(candidates, fileLink{full, text})
	}

	// Скачиваем кандидатов; zip распаковываем и ищем внутри
	for _, c := range candidates {
		path, err := downloadFile(c.url, c.text)
		if err != nil {
			continue
		}
		if strings.ToLower(filepath.Ext(path)) == ".zip" {
			if p, err := extractContract(path); err == nil {
				return p, meta, nil
			}
			continue
		}
		if isContractFile(filepath.Base(path)) {
			return path, meta, nil
		}
		os.Remove(path)
	}
	return "", meta, fmt.Errorf("файл контракта (pdf/docx со словом «контракт» или «договор») не найден в закупке %s", tenderNumber)
}

// field ищет на странице «метка → значение» и возвращает значение без HTML-тегов
func field(page string, labels ...string) string {
	for _, label := range labels {
		re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(label) + `\s*</(?:span|div)>\s*<(?:span|div)[^>]*>(.*?)</(?:span|div)>`)
		if m := re.FindStringSubmatch(page); m != nil {
			s := html.UnescapeString(reTags.ReplaceAllString(m[1], " "))
			if s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " ")); s != "" {
				return s
			}
		}
	}
	return ""
}

func parsePrice(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ',' || r == '.':
			b.WriteRune('.')
		}
	}
	v, _ := strconv.ParseFloat(b.String(), 64)
	return v
}

func isContractFile(name string) bool {
	n := strings.ToLower(name)
	ext := filepath.Ext(n)
	return (ext == ".pdf" || ext == ".docx") && (strings.Contains(n, "контракт") || strings.Contains(n, "договор"))
}

// extractContract достаёт из zip первый pdf/docx со словом «контракт»/«договор»
func extractContract(zipPath string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Base(f.Name) // Base защищает от zip-slip
		if f.FileInfo().IsDir() || !isContractFile(name) {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return "", err
		}
		defer src.Close()
		out := filepath.Join(TmpDir, name)
		dst, err := os.Create(out)
		if err != nil {
			return "", err
		}
		defer dst.Close()
		if _, err := io.Copy(dst, src); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("в архиве нет файла контракта")
}

func httpGet(u string) (*http.Response, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// Без браузерного User-Agent ЕИС часто отвечает ошибкой
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: статус %d", u, resp.StatusCode)
	}
	return resp, nil
}

func getPage(u string) (string, error) {
	resp, err := httpGet(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func downloadFile(link, fallbackName string) (string, error) {
	resp, err := httpGet(link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	name := fallbackName
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
		if dec, err := url.PathUnescape(name); err == nil {
			name = dec
		}
	}
	name = filepath.Base(name)
	if err := os.MkdirAll(TmpDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(TmpDir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return path, nil
}

const certsDir = "certs"

func newClient() *http.Client {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	files, _ := os.ReadDir(certsDir)
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name()))
		if f.IsDir() || (ext != ".crt" && ext != ".pem" && ext != ".cer") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(certsDir, f.Name()))
		if err != nil {
			continue
		}
		if !pool.AppendCertsFromPEM(data) { // не PEM — пробуем как DER
			if cert, err := x509.ParseCertificate(data); err == nil {
				pool.AddCert(cert)
			}
		}
	}
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}
}
