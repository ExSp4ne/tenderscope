package main

import (
	"flag"
	"fmt"
	"os"
	"tenderscope/backend/eis"
)

func main() {
	number := flag.String("number", "", "номер закупки (19 цифр для 44-ФЗ или 11 для 223-ФЗ)")
	flag.Parse()

	path, meta, err := eis.DownloadTenderFiles(*number)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	fmt.Printf("Файл:     %s\nНазвание: %s\nЗаказчик: %s\nЦена:     %.2f\nЗакон:    %s\n",
		path, meta.Title, meta.Customer, meta.Price, meta.Law)
}
