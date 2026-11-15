package eis

import (
	"archive/zip"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Сколько файлов максимум скачиваем со страницы документов, пока ищем контракт
const maxDownloads = 20

// TenderMeta — метаданные закупки (поля совпадают с docs/contracts.md)
type TenderMeta struct {
	Title    string  `json:"title"`
	Customer string  `json:"customer"`
	Price    float64 `json:"price"`
	Law      string  `json:"lawType"` // "44-ФЗ" или "223-ФЗ"
}

var (
	re44     = regexp.MustCompile(`^\d{19}$`)
	re223    = regexp.MustCompile(`^\d{11}$`)
	reTags   = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces = regexp.MustCompile(`\s+`)
	reLink   = regexp.MustCompile(`(?s)<a[^>]+href="([^"]*(?:filestore|download)[^"]*)"[^>]*>(.*?)</a>`)

	clientOnce sync.Once
	client     *http.Client
)

// Типы процедур 44-ФЗ в адресах ЕИС
var noticeTypes44 = []string{"ea20", "ea44", "ok20", "ok44", "ok504", "zk20", "zk504", "ep20", "ep44"}

type fileLink struct{ url, text string }

// DownloadTenderFiles получает метаданные закупки и скачивает файл проекта контракта в TmpDir()/<номер закупки>/
// Возвращает абсолютный путь к файлу
func DownloadTenderFiles(tenderNumber string) (filePath string, meta TenderMeta, err error) {
	var page, docsURL string
	switch {
	case re44.MatchString(tenderNumber):
		meta.Law = "44-ФЗ"
		for _, t := range noticeTypes44 {
			base := "https://zakupki.gov.ru/epz/order/notice/" + t + "/view/"
			if p, e := getPage(base + "common-info.html?regNumber=" + tenderNumber); e == nil {
				page = p
				docsURL = base + "documents.html?regNumber=" + tenderNumber
				break
			}
		}
		if page == "" {
			return "", meta, fmt.Errorf("закупка %s не найдена в ЕИС (проверьте номер: откройте его на zakupki.gov.ru)", tenderNumber)
		}
	case re223.MatchString(tenderNumber):
		meta.Law = "223-ФЗ"
		infoURL := "https://zakupki.gov.ru/223/purchase/public/purchase/info/common-info.html?regNumber=" + tenderNumber
		docsURL = "https://zakupki.gov.ru/223/purchase/public/purchase/info/documents.html?regNumber=" + tenderNumber
		page, err = getPage(infoURL)
		if err != nil {
			return "", meta, err
		}
	default:
		return "", meta, fmt.Errorf("неверный номер закупки: нужно 19 цифр (44-ФЗ) или 11 цифр (223-ФЗ)")
	}

	// Метаданные
	meta.Title = field(page, "Объект закупки", "Наименование закупки")
	meta.Customer = field(page, "Заказчик", "Наименование заказчика")
	meta.Price = parsePrice(field(page, "Начальная цена контракта", "Начальная (максимальная) цена договора", "Начальная (максимальная) цена контракта"))
	if meta.Title == "" {
		return "", meta, fmt.Errorf("не удалось разобрать страницу закупки %s (изменилась вёрстка ЕИС?)", tenderNumber)
	}

	// Ссылки на документы: сначала те, где в тексте «контракт»/«договор», потом все остальные
	// (контракт может лежать внутри архива «Документация об аукционе»)
	docs, err := getPage(docsURL)
	if err != nil {
		return "", meta, err
	}
	links := collectLinks(docs, docsURL)
	sort.SliceStable(links, func(i, j int) bool {
		return hasContractWord(links[i].text) && !hasContractWord(links[j].text)
	})

	dir := filepath.Join(TmpDir(), tenderNumber)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", meta, err
	}

	// Скачиваем, определяем тип по содержимому, zip распаковываем
	for i, l := range links {
		if i >= maxDownloads {
			break
		}
		path, err := downloadFile(l.url, l.text, dir)
		if err != nil {
			continue
		}
		if strings.ToLower(filepath.Ext(path)) == ".zip" {
			p, err := extractContract(path, dir)
			os.Remove(path)
			if err == nil {
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

// collectLinks достаёт со страницы все ссылки на файлы (без дублей)
func collectLinks(page, pageURL string) []fileLink {
	base, _ := url.Parse(pageURL)
	seen := map[string]bool{}
	var res []fileLink
	for _, m := range reLink.FindAllStringSubmatch(page, -1) {
		href, err := url.Parse(html.UnescapeString(m[1]))
		if err != nil {
			continue
		}
		full := base.ResolveReference(href).String()
		text := strings.TrimSpace(reSpaces.ReplaceAllString(html.UnescapeString(reTags.ReplaceAllString(m[2], " ")), " "))
		if seen[full] || skipByName(text) {
			continue
		}
		seen[full] = true
		res = append(res, fileLink{full, text})
	}
	return res
}

// skipByName отсеивает заведомо ненужные файлы (таблицы, картинки, подписи)
func skipByName(text string) bool {
	switch strings.ToLower(filepath.Ext(text)) {
	case ".xls", ".xlsx", ".jpg", ".jpeg", ".png", ".sig", ".xml":
		return true
	}
	return false
}

func hasContractWord(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "контракт") || strings.Contains(s, "договор")
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
	ext := strings.ToLower(filepath.Ext(name))
	return (ext == ".pdf" || ext == ".docx") && hasContractWord(name)
}

// detectExt определяет тип файла по содержимому: ".pdf", ".docx", ".zip" или "" (неизвестно)
func detectExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	head := make([]byte, 4)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil {
		return ""
	}
	if string(head) == "%PDF" {
		return ".pdf"
	}
	if string(head[:2]) == "PK" {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return ".zip"
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name == "word/document.xml" { // внутренность docx
				return ".docx"
			}
		}
		return ".zip"
	}
	return ""
}

// extractContract достаёт из zip первый pdf/docx со словом «контракт»/«договор»
func extractContract(zipPath, dir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := sanitizeName(filepath.Base(f.Name)) // Base защищает от zip-slip
		if f.FileInfo().IsDir() || !isContractFile(name) {
			continue
		}
		out := filepath.Join(dir, name)
		if err := extractFile(f, out); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("в архиве нет файла контракта")
}

func extractFile(f *zip.File, out string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// sanitizeName убирает из имени символы, недопустимые в Windows, и пустые имена
func sanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// newClient: системные сертификаты + все .crt/.pem/.cer из CertsDir()
// Если сертификатов нет — пишем предупреждение в лог
func newClient() *http.Client {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	dir := CertsDir()
	loaded := 0
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("eis: папка сертификатов %s недоступна: %v", dir, err)
	}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name()))
		if f.IsDir() || (ext != ".crt" && ext != ".pem" && ext != ".cer") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			log.Printf("eis: не удалось прочитать сертификат %s: %v", f.Name(), err)
			continue
		}
		if pool.AppendCertsFromPEM(data) {
			loaded++
		} else if cert, err := x509.ParseCertificate(data); err == nil { // не PEM — пробуем как DER
			pool.AddCert(cert)
			loaded++
		} else {
			log.Printf("eis: файл %s не похож на сертификат", f.Name())
		}
	}
	if loaded == 0 {
		log.Printf("eis: сертификаты Минцифры не загружены из %s — запросы к ЕИС могут падать с ошибкой x509 (путь можно задать через TENDERSCOPE_CERTS_DIR)", dir)
	}
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}
}

func httpClient() *http.Client {
	clientOnce.Do(func() { client = newClient() })
	return client
}

func httpGet(u string) (*http.Response, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// Без браузерного User-Agent ЕИС часто отвечает ошибкой
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	resp, err := httpClient().Do(req)
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

// downloadFile скачивает файл в dir. Если расширение в имени отсутствует или не совпадает
// с содержимым, добавляет правильное (.pdf / .docx / .zip), определив тип по первым байтам
func downloadFile(link, fallbackName, dir string) (string, error) {
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
	path := filepath.Join(dir, sanitizeName(filepath.Base(name)))

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(path)
		return "", err
	}

	if ext := detectExt(path); ext != "" && strings.ToLower(filepath.Ext(path)) != ext {
		fixed := path + ext // «Проект контракта» -> «Проект контракта.docx»
		if err := os.Rename(path, fixed); err != nil {
			return "", err
		}
		path = fixed
	}
	return path, nil
}
