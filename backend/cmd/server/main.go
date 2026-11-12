package main

import (
	"log"
	"net/http"
	"tenderscope/backend/eis"
)

// Временный сервер, чтобы проверить /api/upload через curl
// Когда появится core от Бэкендера 2 - просто подключить туда eis.UploadHandler
func main() {
	http.HandleFunc("/api/upload", eis.UploadHandler)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
