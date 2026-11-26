package eis

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- помощники ----------

type zf struct {
	name string
	data []byte
}

func makeZip(t *testing.T, files ...zf) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeDocx — минимальный zip, который detectExt принимает за docx (есть word/document.xml)
func fakeDocx(t *testing.T) []byte {
	t.Helper()
	return makeZip(t,
		zf{"[Content_Types].xml", []byte(`<Types/>`)},
		zf{"word/document.xml", []byte(`<w:document/>`)},
	)
}

// makeRar4 собирает минимальный RAR 4.x без сжатия (method 0x30) — для тестов без утилиты rar
func makeRar4(t *testing.T, files ...zf) []byte {
	t.Helper()
	le16 := func(v int) []byte { return []byte{byte(v), byte(v >> 8)} }
	le32 := func(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
	block := func(b *bytes.Buffer, body []byte) { // CRC16 = младшие 16 бит CRC32 от тела
		b.Write(le16(int(crc32.ChecksumIEEE(body) & 0xFFFF)))
		b.Write(body)
	}

	var b bytes.Buffer
	b.Write([]byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00})
	block(&b, []byte{0x73, 0, 0, 13, 0, 0, 0, 0, 0, 0, 0}) // главный заголовок
	for _, f := range files {
		var h bytes.Buffer
		h.WriteByte(0x74)                  // тип: файл
		h.Write(le16(0x8000))              // флаги: есть данные
		h.Write(le16(32 + len(f.name)))    // размер заголовка (с CRC)
		h.Write(le32(uint32(len(f.data)))) // packed size
		h.Write(le32(uint32(len(f.data)))) // unpacked size
		h.WriteByte(0)                     // host OS
		h.Write(le32(crc32.ChecksumIEEE(f.data)))
		h.Write(le32(0))           // время
		h.WriteByte(20)            // версия распаковки
		h.WriteByte(0x30)          // метод: store
		h.Write(le16(len(f.name))) // длина имени
		h.Write(le32(0))           // атрибуты
		h.WriteString(f.name)      // имя (UTF-8)
		block(&b, h.Bytes())
		b.Write(f.data)
	}
	b.Write([]byte{0xC4, 0x3D, 0x7B, 0x00, 0x40, 0x07, 0x00}) // конец архива
	return b.Bytes()
}

// setupEnv направляет TmpDir/UploadDir/CertsDir во временные папки теста
func setupEnv(t *testing.T) (tmp, upload string) {
	t.Helper()
	tmp, upload = t.TempDir(), t.TempDir()
	t.Setenv("TENDERSCOPE_TMP_DIR", tmp)
	t.Setenv("TENDERSCOPE_UPLOAD_DIR", upload)
	t.Setenv("TENDERSCOPE_CERTS_DIR", t.TempDir())
	return tmp, upload
}

func contentDisposition(name string) string {
	return "attachment; filename*=UTF-8''" + url.PathEscape(name)
}

type mockFile struct {
	linkText string // текст ссылки на странице документов
	name     string // имя в Content-Disposition
	data     []byte
}

// mockOmitDocsLink: страница закупки 223-ФЗ без ссылки на «Документы» (проверка запасного адреса)
var mockOmitDocsLink bool

const commonInfoHTML = `<html><body>
<div><span class="section__title">Объект закупки</span><span class="section__info">Поставка бумаги</span></div>
<div><span class="section__title">Заказчик</span><span class="section__info">ГБУ Гормост</span></div>
<div><span class="section__title">Начальная цена контракта</span><span class="section__info">14 500 000,00 ₽</span></div>
</body></html>`

// newMockEIS поднимает фейковый ЕИС: страница закупки (44 или 223), страница документов и файлы.
// Возвращает сервер и список путей, к которым ходил клиент.
func newMockEIS(t *testing.T, law string, regNumber string, files []mockFile) (*httptest.Server, *[]string) {
	t.Helper()
	var hits []string
	mux := http.NewServeMux()

	infoPath, docsPath := "/epz/order/notice/ea44/view/common-info.html", "/epz/order/notice/ea44/view/documents.html"
	if law == Law223 {
		infoPath = "/223/purchase/public/purchase/info/common-info.html"
		docsPath = "/epz/order/notice/notice223/documents.html"
	}
	mux.HandleFunc(infoPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("regNumber") != regNumber {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := commonInfoHTML
		if law == Law223 && !mockOmitDocsLink {
			page = strings.Replace(page, "</body>", `<a href="/epz/order/notice/notice223/documents.html?noticeInfoId=777&noticeGuid=abc-123">Документы</a></body>`, 1)
		}
		w.Write([]byte(page))
	})
	mux.HandleFunc(docsPath, func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("<html><body>")
		for i, f := range files {
			fmt.Fprintf(&b, `<a href="/filestore/public/1.0/download/f%d?uid=%d">%s</a>`, i, i, f.linkText)
		}
		b.WriteString(`<a href="/filestore/public/1.0/download/table.xlsx">Расчёт.xlsx</a>`) // skipByName
		b.WriteString("</body></html>")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(b.String()))
	})
	for i, f := range files {
		f := f
		mux.HandleFunc(fmt.Sprintf("/filestore/public/1.0/download/f%d", i), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Disposition", contentDisposition(f.name))
			w.Write(f.data)
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.RequestURI())
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	old := eisBaseURL
	eisBaseURL = srv.URL
	t.Cleanup(func() { eisBaseURL = old })
	return srv, &hits
}

// ---------- выбор файла и lawType ----------

func TestContractScore(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"Проект контракта.docx", scoreProject},
		{"Проект договора.pdf", scoreProject},
		{"ПРОЕКТ_КОНТРАКТА_финал.docx", scoreProject},
		{"Проект государственного контракта.docx", scoreProject},
		{"Проект МК на оказание услуг версия 1.01.docx", scoreProject}, // реальный пример из ЕИС
		{"Проект ГК.pdf", scoreProject},
		{"Проект МКУ.docx", -1}, // МК должно быть отдельным словом
		{"Протокол по проекту МК.docx", -1},
		{"Аукционная документация.docx", -1},
		{"Контракт.docx", scoreContract},
		{"Договор поставки.pdf", scoreContract},
		{"Протокол разногласий к проекту контракта.docx", -1},
		{"Обоснование НМЦК (проект договора).docx", -1},
		{"Инструкция по заполнению контракта.pdf", -1},
		{"Проект контракта.doc", -1}, // не pdf/docx
		{"Проект контракта.xlsx", -1},
		{"Извещение.pdf", -1},
	}
	for _, c := range cases {
		if got := contractScore(c.name); got != c.want {
			t.Errorf("contractScore(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestValidateLawType(t *testing.T) {
	for _, s := range []string{"44-ФЗ", "223-ФЗ"} {
		if err := ValidateLawType(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"", "44-фз", "44-FZ", " 44-ФЗ", "44-ФЗ ", "44ФЗ", "94-ФЗ", "44-ФЗ\n"} {
		if ValidateLawType(s) == nil {
			t.Errorf("%q должен отклоняться", s)
		}
	}
}

func TestUnescapeHref(t *testing.T) {
	cases := map[string]string{
		"/a?x=1&noticeGuid=2":     "/a?x=1&noticeGuid=2", // голый & не трогаем: html.UnescapeString дал бы «¬iceGuid»
		"/a?x=1&amp;noticeGuid=2": "/a?x=1&noticeGuid=2",
		"/a?x=1&#38;y=2":          "/a?x=1&y=2",
		"/a?uid=ABC&copy=1&reg=2": "/a?uid=ABC&copy=1&reg=2", // «&copy», «&reg» тоже не сущности в href
	}
	for in, want := range cases {
		if got := unescapeHref(in); got != want {
			t.Errorf("unescapeHref(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeCP866(t *testing.T) {
	// «Договор Ёж.docx» в CP866
	raw := []byte{0x84, 0xAE, 0xA3, 0xAE, 0xA2, 0xAE, 0xE0, ' ', 0xF0, 0xA6, '.', 'd', 'o', 'c', 'x'}
	if got := decodeCP866(string(raw)); got != "Договор Ёж.docx" {
		t.Errorf("got %q", got)
	}
}

// ---------- распаковка ----------

func writeZip(t *testing.T, data []byte) (zipPath, outDir string) {
	t.Helper()
	root := t.TempDir()
	zipPath = filepath.Join(root, "in.zip")
	if err := os.WriteFile(zipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	outDir = filepath.Join(root, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return zipPath, outDir
}

func TestExtractContract_NestedZipAndPriority(t *testing.T) {
	inner := makeZip(t,
		zf{"Протокол разногласий.docx", fakeDocx(t)},
		zf{"Проект контракта.docx", fakeDocx(t)},
	)
	outer := makeZip(t,
		zf{"Извещение.pdf", []byte("%PDF")},
		zf{"Договор.docx", fakeDocx(t)}, // менее приоритетный, лежит на верхнем уровне
		zf{"docs/Приложения.zip", inner},
	)
	zipPath, out := writeZip(t, outer)

	p, score, err := extractContract(zipPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "Проект контракта.docx" || score != scoreProject {
		t.Fatalf("got %q score=%d", p, score)
	}
	// остальные кандидаты и временные архивы не должны остаться на диске
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Errorf("в папке должен остаться один файл, есть: %v", entries)
	}
}

func TestExtractContract_NoContract(t *testing.T) {
	zipPath, out := writeZip(t, makeZip(t, zf{"Протокол.docx", fakeDocx(t)}, zf{"Извещение.pdf", []byte("%PDF")}))
	if _, _, err := extractContract(zipPath, out); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestExtractContract_ZipSlipStaysInside(t *testing.T) {
	zipPath, out := writeZip(t, makeZip(t, zf{"../../Договор.docx", fakeDocx(t)}))
	p, _, err := extractContract(zipPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != out {
		t.Errorf("файл вышел за пределы папки: %s", p)
	}
	if fileExists(filepath.Join(filepath.Dir(out), "Договор.docx")) {
		t.Error("файл записан рядом с папкой распаковки")
	}
}

func TestExtractContract_DepthLimit(t *testing.T) {
	data := makeZip(t, zf{"Договор.docx", fakeDocx(t)})
	for i := 0; i < maxZipDepth+2; i++ {
		data = makeZip(t, zf{fmt.Sprintf("level%d.zip", i), data})
	}
	zipPath, out := writeZip(t, data)
	if _, _, err := extractContract(zipPath, out); err == nil {
		t.Fatal("слишком глубокая вложенность должна давать ошибку")
	}
}

func TestExtractContract_Rar(t *testing.T) {
	data := makeRar4(t,
		zf{"Извещение.pdf", []byte("%PDF")},
		zf{"Договор.docx", fakeDocx(t)},
		zf{"Проект договора.docx", fakeDocx(t)},
	)
	zipPath, out := writeZip(t, data) // формат определяется по содержимому, а не по имени
	p, score, err := extractContract(zipPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "Проект договора.docx" || score != scoreProject {
		t.Fatalf("got %q score=%d", p, score)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, fakeDocx(t)) {
		t.Error("содержимое файла из rar не совпадает")
	}
}

func TestExtractContract_ZipInsideRarInsideZip(t *testing.T) {
	inner := makeZip(t, zf{"Проект контракта.docx", fakeDocx(t)})
	rar := makeRar4(t, zf{"Приложения.zip", inner})
	outer := makeZip(t, zf{"Документы.rar", rar})
	zipPath, out := writeZip(t, outer)
	p, _, err := extractContract(zipPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "Проект контракта.docx" {
		t.Fatalf("got %q", p)
	}
}

func TestExtractContract_CorruptRar(t *testing.T) {
	zipPath, out := writeZip(t, []byte("Rar!\x1a\x07\x00 это не архив"))
	if _, _, err := extractContract(zipPath, out); err == nil {
		t.Fatal("битый rar должен давать ошибку")
	}
}

// ---------- полный цикл DownloadTenderFiles ----------

func TestDownloadTenderFiles_44_NestedZip(t *testing.T) {
	tmp, _ := setupEnv(t)
	const num = "0373200009324000123"

	inner := makeZip(t,
		zf{"Протокол разногласий.docx", fakeDocx(t)},
		zf{"Обоснование НМЦК договор.docx", fakeDocx(t)},
		zf{"Проект договора.docx", fakeDocx(t)},
	)
	archive := makeZip(t, zf{"Документация/Приложения.zip", inner}, zf{"Извещение.pdf", []byte("%PDF-1.4")})

	_, hits := newMockEIS(t, Law44, num, []mockFile{
		{"Документация об аукционе", "docs.zip", archive},
		{"Инструкция по договору", "Инструкция по договору.docx", fakeDocx(t)}, // исключается
	})

	path, meta, err := DownloadTenderFiles(num)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Проект договора.docx" {
		t.Errorf("выбран %q", path)
	}
	if !strings.HasPrefix(path, filepath.Join(tmp, num)) {
		t.Errorf("файл вне TmpDir/<номер>: %s", path)
	}
	if !fileExists(path) {
		t.Error("файл не существует")
	}
	want := TenderMeta{Title: "Поставка бумаги", Customer: "ГБУ Гормост", Price: 14500000, Law: Law44}
	if meta != want {
		t.Errorf("meta = %+v, want %+v", meta, want)
	}
	// ea20 в списке типов идёт раньше ea44 — клиент должен был перебрать типы до найденного
	var sawInfo bool
	for _, h := range *hits {
		if strings.Contains(h, "/ea44/view/common-info.html") {
			sawInfo = true
		}
	}
	if !sawInfo {
		t.Errorf("не было запроса к ea44: %v", *hits)
	}
}

func TestDownloadTenderFiles_PrefersProjectOverPlainContract(t *testing.T) {
	setupEnv(t)
	const num = "0373200009324000124"

	// «Договор» лежит отдельной ссылкой и идёт первым (в тексте есть слово договор),
	// «Проект контракта» — внутри архива. Должен победить проект.
	archive := makeZip(t, zf{"Проект контракта.docx", fakeDocx(t)})
	newMockEIS(t, Law44, num, []mockFile{
		{"Договор", "Договор.docx", fakeDocx(t)},
		{"Документация", "docs.zip", archive},
	})

	path, _, err := DownloadTenderFiles(num)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Проект контракта.docx" {
		t.Errorf("выбран %q", path)
	}
}

func TestDownloadTenderFiles_SameNameFilesDontClash(t *testing.T) {
	setupEnv(t)
	const num = "0373200009324000125"

	a, b := fakeDocx(t), append(fakeDocx(t), 0) // разное содержимое при одинаковых именах
	newMockEIS(t, Law44, num, []mockFile{
		{"Договор (1)", "Договор.docx", a},
		{"Договор (2)", "Договор.docx", b},
	})
	path, _, err := DownloadTenderFiles(num)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, a) {
		t.Errorf("первый файл был затёрт вторым (err=%v)", err)
	}
}

func TestDownloadTenderFiles_223(t *testing.T) {
	setupEnv(t)
	const num = "32312345678"

	_, hits := newMockEIS(t, Law223, num, []mockFile{
		{"Проект договора", "Проект договора.docx", fakeDocx(t)},
	})
	path, meta, err := DownloadTenderFiles(num)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Проект договора.docx" || meta.Law != Law223 {
		t.Errorf("path=%q law=%q", path, meta.Law)
	}
	// страница документов берётся из ссылки на странице закупки (noticeInfoId; «голый» & в href не должен портиться в «¬»)
	if !containsHit(*hits, "/epz/order/notice/notice223/documents.html?noticeInfoId=777&noticeGuid=abc-123") {
		t.Errorf("не было запроса по ссылке со страницы закупки: %v", *hits)
	}
	if err := ValidateLawType(meta.Law); err != nil {
		t.Error(err)
	}
}

func containsHit(hits []string, want string) bool {
	for _, h := range hits {
		if h == want {
			return true
		}
	}
	return false
}

func TestDownloadTenderFiles_223_FallbackDocsURL(t *testing.T) {
	setupEnv(t)
	const num = "32312345679"
	mockOmitDocsLink = true
	t.Cleanup(func() { mockOmitDocsLink = false })

	_, hits := newMockEIS(t, Law223, num, []mockFile{
		{"Договор", "Договор.pdf", []byte("%PDF-1.4")},
	})
	if _, _, err := DownloadTenderFiles(num); err != nil {
		t.Fatal(err)
	}
	want := "/epz/order/notice/notice223/documents.html?purchaseNoticeNumber=" + num
	if !containsHit(*hits, want) {
		t.Errorf("ожидался запрос %s, было: %v", want, *hits)
	}
}

// Реальный случай из ЕИС: контракт лежит в .rar, имя файла «Прил.2 к извещению (проект контракта).rar»
func TestDownloadTenderFiles_RarContract(t *testing.T) {
	setupEnv(t)
	const num = "0311100007226000046"

	rar := makeRar4(t,
		zf{"Проект контракта.docx", fakeDocx(t)},
		zf{"Протокол.docx", fakeDocx(t)},
	)
	newMockEIS(t, Law44, num, []mockFile{
		{"Прил.1 к извещению (обоснование НМЦ)", "Прил.1 к извещению (обоснование НМЦ).rar", makeRar4(t, zf{"НМЦ.docx", fakeDocx(t)})},
		{"Прил.2 к извещению (проект контракта)", "Прил.2 к извещению (проект контракта).rar", rar},
	})
	path, _, err := DownloadTenderFiles(num)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Проект контракта.docx" {
		t.Errorf("выбран %q", path)
	}
}

func TestDownloadTenderFiles_NoContract(t *testing.T) {
	setupEnv(t)
	const num = "0373200009324000126"
	newMockEIS(t, Law44, num, []mockFile{
		{"Протокол", "Протокол.docx", fakeDocx(t)},
		{"Документация", "docs.zip", makeZip(t, zf{"Обоснование.docx", fakeDocx(t)})},
	})
	if _, _, err := DownloadTenderFiles(num); err == nil {
		t.Fatal("ожидалась ошибка «файл контракта не найден»")
	}
}

func TestDownloadTenderFiles_BadNumber(t *testing.T) {
	setupEnv(t)
	// сервер не нужен: номер отсекается до сетевых запросов
	for _, n := range []string{"", "123", "abc", "0373200009324000123 "} {
		if _, _, err := DownloadTenderFiles(n); err == nil {
			t.Errorf("%q должен отклоняться", n)
		}
	}
}

// ---------- очистка ----------

func TestCleanupOldFiles(t *testing.T) {
	tmp, up := setupEnv(t)
	old := time.Now().Add(-48 * time.Hour)

	mustDir := func(p string) { t.Helper(); os.MkdirAll(filepath.Join(tmp, p), 0o755) }
	mustFile := func(dir, name string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mustDir("old-tender")
	mustFile(filepath.Join(tmp, "old-tender"), "a.docx")
	mustDir("fresh-tender")
	oldFile, freshFile := mustFile(up, "old.docx"), mustFile(up, "fresh.docx")
	strayTmpFile := mustFile(tmp, "stray.txt") // файлы в TmpDir не трогаем: чистим только подпапки
	for _, p := range []string{filepath.Join(tmp, "old-tender"), oldFile, strayTmpFile} {
		os.Chtimes(p, old, old)
	}

	if err := CleanupOldFiles(24 * time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(tmp, "old-tender"), oldFile} {
		if fileExists(p) {
			t.Errorf("%s должен быть удалён", p)
		}
	}
	for _, p := range []string{filepath.Join(tmp, "fresh-tender"), freshFile, strayTmpFile} {
		if !fileExists(p) {
			t.Errorf("%s должен остаться", p)
		}
	}
}

func TestCleanupOldFiles_MissingDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TENDERSCOPE_TMP_DIR", filepath.Join(base, "no-tmp"))
	t.Setenv("TENDERSCOPE_UPLOAD_DIR", filepath.Join(base, "no-uploads"))
	if err := CleanupOldFiles(time.Hour); err != nil {
		t.Fatalf("отсутствующие папки не ошибка: %v", err)
	}
}
