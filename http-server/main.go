package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	http.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello ! "+r.FormValue("ip"))
	})

	http.HandleFunc("/api/getDate", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, time.Now())
	})

	http.HandleFunc("/api/upload", uploadHandle)

	http.Handle("/staticfile/", http.StripPrefix("/staticfile/", http.FileServer(http.Dir("./upload"))))

	http.ListenAndServe(":9999", nil)
}

func uploadHandle(w http.ResponseWriter, r *http.Request) {
	// 根据字段名获取表单文件
	formFile, header, err := r.FormFile("uploadfile")
	if err != nil {
		log.Printf("Get form file failed: %s\n", err)
		return
	}
	defer formFile.Close()
	// 创建保存文件
	destFile, err := os.Create("./upload/" + header.Filename)
	if err != nil {
		log.Printf("Create failed: %s\n", err)
		return
	}
	defer destFile.Close()

	// 读取表单文件，写入保存文件
	_, err = io.Copy(destFile, formFile)
	if err != nil {
		log.Printf("Write file failed: %s\n", err)
		return
	}
	fmt.Fprintln(w, "上传文件成功")
}
