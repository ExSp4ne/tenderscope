package eis

import (
	"archive/zip"
	"crypto/tls"
	"crypto/x509"
	"errors"
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

	"github.com/nwaples/rardecode/v2"
)

// Сколько файлов максимум скачиваем со страницы документов, пока ищем контракт
const maxDownloads = 20

// Лимиты при распаковке архивов (защита от zip-bomb и бесконечной вложенности)
const (
	maxZipDepth     = 3         // архив в архиве в архиве
	maxZipEntries   = 2000      // записей во всех архивах суммарно
	maxUnpackedSize = 500 << 20 // 500 МБ суммарно распакованных данных
)

// Допустимые значения lawType (docs/contracts.md)
const (
	Law44  = "44-ФЗ"
	Law223 = "223-ФЗ"
)

// Баллы приоритета файла контракта
const (
	scoreProject  = 100 // «проект контракта» / «проект договора»
	scoreContract = 10  // просто «контракт» / «договор»
)

// eisBaseURL — адрес ЕИС; переменная, чтобы в тестах подменять на httptest-сервер
var eisBaseURL = "https://zakupki.gov.ru"

// debugf пишет подробности работы загрузчика, если задана переменная TENDERSCOPE_DEBUG=1
func debugf(format string, a ...any) {
	if os.Getenv("TENDERSCOPE_DEBUG") != "" {
		log.Printf("eis: "+format, a...)
	}
}

var errZipLimit = errors.New("превышен лимит распаковки архива")

// ValidateLawType проверяет lawType строго по docs/contracts.md: только "44-ФЗ" или "223-ФЗ",
// без trim и без смены регистра
func ValidateLawType(s string) error {
	if s != Law44 && s != Law223 {
		return fmt.Errorf("недопустимый lawType %q: ожидается %q или %q", s, Law44, Law223)
	}
	return nil
}

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
	// ссылка на вкладку «Документы» на странице закупки 223-ФЗ (содержит noticeInfoId)
	reDocs223 = regexp.MustCompile(`href="([^"]*notice223/documents\.html[^"]*)"`)
	// «проект контракта», «проект договора», «проект государственного контракта»,
	// а также сокращения «проект МК» (муниципальный контракт) и «проект ГК» (государственный контракт).
	// \b для кириллицы в RE2 не работает, поэтому граница слова задана через \p{L}
	reProject = regexp.MustCompile(`проект\s+(?:\S+\s+){0,2}?(?:контракта|договора|мк|гк)(?:[^\p{L}]|$)`)

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
		meta.Law = Law44
		var lastErr error
		for _, t := range noticeTypes44 {
			base := eisBaseURL + "/epz/order/notice/" + t + "/view/"
			p, e := getPage(base + "common-info.html?regNumber=" + tenderNumber)
			if e == nil {
				page = p
				docsURL = base + "documents.html?regNumber=" + tenderNumber
				debugf("закупка найдена как %s", t)
				break
			}
			debugf("тип %s: %v", t, e)
			lastErr = e
		}
		if page == "" {
			return "", meta, fmt.Errorf("закупка %s не найдена в ЕИС (проверьте номер: откройте его на zakupki.gov.ru); последняя ошибка: %v", tenderNumber, lastErr)
		}
	case re223.MatchString(tenderNumber):
		meta.Law = Law223
		// Страница закупки: сначала привычный адрес, затем новый (notice223)
		infoURLs := []string{
			eisBaseURL + "/223/purchase/public/purchase/info/common-info.html?regNumber=" + tenderNumber,
			eisBaseURL + "/epz/order/notice/notice223/common-info.html?regNumber=" + tenderNumber,
		}
		var lastErr error
		for _, u := range infoURLs {
			if page, lastErr = getPage(u); lastErr == nil {
				break
			}
			debugf("%v", lastErr)
		}
		if lastErr != nil {
			return "", meta, lastErr
		}
		docsURL = docsURL223(page, tenderNumber)
		debugf("страница документов 223-ФЗ: %s", docsURL)
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
	debugf("страница документов %s: найдено ссылок на файлы: %d", docsURL, len(links))
	for _, l := range links {
		debugf("  ссылка: %q -> %s", l.text, l.url)
	}
	sort.SliceStable(links, func(i, j int) bool {
		return looksLikeContract(links[i].text) && !looksLikeContract(links[j].text)
	})

	dir := filepath.Join(TmpDir(), tenderNumber)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", meta, err
	}

	// Скачиваем, определяем тип по содержимому, zip (в том числе вложенные) распаковываем.
	// Каждая ссылка — в свою подпапку, чтобы одноимённые файлы не затирали друг друга.
	// Выбираем файл с наибольшим приоритетом; «проект контракта/договора» — сразу выходим.
	var best string
	bestScore, downloaded := -1, 0
	consider := func(p string, score int) {
		if score > bestScore {
			if best != "" {
				os.Remove(best)
			}
			best, bestScore = p, score
			return
		}
		os.Remove(p)
	}
	for i, l := range links {
		if i >= maxDownloads {
			break
		}
		work := filepath.Join(dir, strconv.Itoa(i))
		if err := os.MkdirAll(work, 0o755); err != nil {
			return "", meta, err
		}
		path, err := downloadFile(l.url, l.text, work)
		if err != nil {
			debugf("не скачан %q: %v", l.text, err)
			os.Remove(work)
			continue
		}
		downloaded++
		if isArchiveName(path) {
			p, score, err := extractContract(path, work)
			os.Remove(path)
			if err == nil {
				debugf("архив %q: выбран %s (приоритет %d)", filepath.Base(path), filepath.Base(p), score)
				consider(p, score)
			} else {
				debugf("архив %q: %v", filepath.Base(path), err)
			}
		} else if score := contractScore(filepath.Base(path)); score >= 0 {
			debugf("файл %q подходит (приоритет %d)", filepath.Base(path), score)
			consider(path, score)
		} else {
			debugf("файл %q пропущен: не pdf/docx контракта", filepath.Base(path))
			os.Remove(path)
		}
		os.Remove(work) // удалится, только если пустая
		if bestScore >= scoreProject {
			break
		}
	}
	if best == "" {
		return "", meta, fmt.Errorf("файл контракта (pdf/docx со словом «контракт»/«договор» или «проект МК/ГК») не найден в закупке %s (ссылок на странице документов: %d, скачано файлов: %d; подробности: TENDERSCOPE_DEBUG=1)", tenderNumber, len(links), downloaded)
	}
	if err := ValidateLawType(meta.Law); err != nil {
		return "", meta, err
	}
	return best, meta, nil
}

// unescapeHref раскодирует только амперсанд в адресе из атрибута href.
// html.UnescapeString здесь нельзя: в адресах часто стоит «голый» &, и «&noticeGuid=»
// превратилось бы в «¬iceGuid=» (браузеры не трогают такие сущности внутри href, а Go — трогает).
var hrefUnescaper = strings.NewReplacer("&amp;", "&", "&#38;", "&", "&#x26;", "&", "&#X26;", "&")

func unescapeHref(s string) string { return hrefUnescaper.Replace(s) }

// docsURL223 берёт адрес вкладки «Документы» со страницы закупки (в нём есть noticeInfoId).
// Если ссылки на странице нет, подставляет адрес по номеру закупки.
func docsURL223(page, number string) string {
	if m := reDocs223.FindStringSubmatch(page); m != nil {
		if href, err := url.Parse(unescapeHref(m[1])); err == nil {
			base, _ := url.Parse(eisBaseURL)
			return base.ResolveReference(href).String()
		}
	}
	return eisBaseURL + "/epz/order/notice/notice223/documents.html?purchaseNoticeNumber=" + number
}

// collectLinks достаёт со страницы все ссылки на файлы (без дублей)
func collectLinks(page, pageURL string) []fileLink {
	base, _ := url.Parse(pageURL)
	seen := map[string]bool{}
	var res []fileLink
	for _, m := range reLink.FindAllStringSubmatch(page, -1) {
		href, err := url.Parse(unescapeHref(m[1]))
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
	s = normName(s)
	return strings.Contains(s, "контракт") || strings.Contains(s, "договор")
}

// looksLikeContract: в тексте есть слово «контракт»/«договор» или «проект МК/ГК»
func looksLikeContract(s string) bool {
	return hasContractWord(s) || reProject.MatchString(normName(s))
}

// Слова, по которым файл точно не является проектом контракта
var excludedWords = []string{"протокол", "обоснование", "инструкция"}

// normName: нижний регистр, «ё» -> «е», «_» -> пробел («Проект_контракта.docx»)
func normName(s string) string {
	return strings.NewReplacer("ё", "е", "Ё", "е", "_", " ").Replace(strings.ToLower(s))
}

// contractScore — приоритет файла как контракта (-1: не подходит).
// Подходят только pdf/docx со словом «контракт»/«договор» без «протокол», «обоснование», «инструкция».
func contractScore(name string) int {
	n := normName(name)
	if ext := filepath.Ext(n); ext != ".pdf" && ext != ".docx" {
		return -1
	}
	for _, w := range excludedWords {
		if strings.Contains(n, w) {
			return -1
		}
	}
	switch {
	case reProject.MatchString(n):
		return scoreProject
	case hasContractWord(n):
		return scoreContract
	}
	return -1
}

func isContractFile(name string) bool { return contractScore(name) >= 0 }

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

// detectExt определяет тип файла по содержимому: ".pdf", ".docx", ".zip", ".rar" или "" (неизвестно)
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
	if string(head) == "Rar!" { // сигнатура RAR 1.5–4.x и 5.x
		return ".rar"
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

// isArchiveName: имя оканчивается на .zip или .rar
func isArchiveName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar":
		return true
	}
	return false
}

// walkArchive обходит файлы zip- или rar-архива (формат определяется по содержимому) и вызывает fn
// для каждого. size — распакованный размер или -1, если он неизвестен. Читать r можно только
// внутри fn. Папки и зашифрованные записи пропускаются.
func walkArchive(path string, fn func(name string, size int64, r io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	head := make([]byte, 4)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil {
		return err
	}
	if string(head) == "Rar!" {
		return walkRar(path, fn)
	}
	return walkZip(path, fn)
}

func walkZip(path string, fn func(string, int64, io.Reader) error) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		size := int64(-1)
		if f.UncompressedSize64 <= uint64(maxUnpackedSize) {
			size = int64(f.UncompressedSize64)
		} else {
			size = maxUnpackedSize + 1 // заведомо больше лимита
		}
		err = fn(zipEntryName(f), size, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func walkRar(path string, fn func(string, int64, io.Reader) error) error {
	rr, err := rardecode.OpenReader(path, rardecode.MaxDictionarySize(256<<20))
	if err != nil {
		return err
	}
	defer rr.Close()
	for {
		h, err := rr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.IsDir || h.Encrypted || h.HeaderEncrypted {
			continue
		}
		size := h.UnPackedSize
		if h.UnKnownSize {
			size = -1
		}
		if err := fn(h.Name, size, rr); err != nil {
			return err
		}
	}
}

// extractContract ищет в архиве (zip или rar, включая вложенные архивы) лучший файл контракта,
// распаковывает только его в dir и возвращает путь и приоритет
func extractContract(archivePath, dir string) (string, int, error) {
	used := 0
	var total int64
	return extractContractRec(archivePath, dir, 0, &used, &total)
}

func extractContractRec(archivePath, dir string, depth int, entries *int, total *int64) (best string, bestScore int, err error) {
	bestScore = -1
	if depth > maxZipDepth {
		return "", -1, fmt.Errorf("слишком глубокая вложенность архивов")
	}

	keep := func(p string, score int) {
		if score > bestScore {
			if best != "" {
				os.Remove(best)
			}
			best, bestScore = p, score
			return
		}
		os.Remove(p)
	}

	walkErr := walkArchive(archivePath, func(name string, size int64, r io.Reader) error {
		if *entries++; *entries > maxZipEntries {
			return errZipLimit
		}
		// Base защищает от zip-slip: на диск пишем только по имени файла
		base := sanitizeName(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
		nested := isArchiveName(base)
		score := contractScore(base)
		if !nested && score < 0 {
			return nil
		}
		out := uniquePath(dir, base)
		if err := writeEntry(r, size, out, total); err != nil {
			os.Remove(out)
			if errors.Is(err, errZipLimit) {
				return err
			}
			return nil // битая запись — пропускаем
		}
		if !nested {
			keep(out, score)
			return nil
		}
		p, s, err := extractContractRec(out, dir, depth+1, entries, total)
		os.Remove(out)
		if errors.Is(err, errZipLimit) {
			return err
		}
		if err == nil {
			keep(p, s)
		}
		return nil
	})
	if errors.Is(walkErr, errZipLimit) {
		if best != "" {
			os.Remove(best)
		}
		return "", -1, walkErr
	}
	if best == "" {
		if walkErr != nil {
			return "", -1, fmt.Errorf("не удалось прочитать архив: %w", walkErr)
		}
		return "", -1, fmt.Errorf("в архиве нет файла контракта")
	}
	return best, bestScore, nil
}

// writeEntry пишет запись архива на диск с учётом общего лимита распакованных данных
func writeEntry(r io.Reader, size int64, out string, total *int64) error {
	if size > maxUnpackedSize {
		return errZipLimit
	}
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer dst.Close()
	n, err := io.Copy(dst, io.LimitReader(r, maxUnpackedSize-*total+1))
	*total += n
	if err != nil {
		return err
	}
	if *total > maxUnpackedSize {
		return errZipLimit
	}
	return nil
}

// uniquePath возвращает свободный путь в dir: «name», «name_1.ext», «name_2.ext»...
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; fileExists(p); i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
	}
	return p
}

// zipEntryName: ЕИС/Windows-архиверы часто кладут имена в CP866 без флага UTF-8
func zipEntryName(f *zip.File) string {
	if !f.NonUTF8 {
		return f.Name
	}
	return decodeCP866(f.Name)
}

func decodeCP866(s string) string {
	r := make([]rune, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c < 0x80:
			r = append(r, rune(c))
		case c <= 0xAF: // А-Я, а-п
			r = append(r, rune(c)-0x80+0x410)
		case c >= 0xE0 && c <= 0xEF: // р-я
			r = append(r, rune(c)-0xE0+0x440)
		case c == 0xF0:
			r = append(r, 'Ё')
		case c == 0xF1:
			r = append(r, 'ё')
		default:
			r = append(r, '_')
		}
	}
	return string(r)
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
