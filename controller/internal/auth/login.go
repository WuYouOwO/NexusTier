package auth

import (
	"html/template"
	"io"
	"net/http"
	"strings"
)

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NexusTier 登录</title>
  <style>
    :root {
      --primary: #1677ff;
      --primary-hover: #0958d9;
      --danger: #ff4d4f;
      --bg: #f0f2f5;
      --surface: #fff;
      --border: #e8eaed;
      --text-1: #1f2937;
      --text-3: #6b7280;
      --radius: 6px;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      min-height: 100vh;
      background: var(--bg);
      display: flex;
      align-items: center;
      justify-content: center;
      font-family: -apple-system, "PingFang SC", "Microsoft YaHei", Arial, sans-serif;
      font-size: 14px;
      color: var(--text-1);
    }
    .card {
      width: 360px;
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 36px 32px 28px;
      box-shadow: 0 4px 16px rgba(0,0,0,.08);
    }
    .brand { display: flex; align-items: center; gap: 10px; margin-bottom: 28px; }
    .logo-mark {
      width: 32px; height: 32px;
      background: var(--primary);
      border-radius: 7px;
      color: #fff;
      font-size: 16px;
      font-weight: 800;
      display: flex; align-items: center; justify-content: center;
      flex-shrink: 0;
    }
    .brand-name { font-size: 18px; font-weight: 700; }
    .brand-sub  { font-size: 12px; color: var(--text-3); }
    .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 16px; }
    .field label { font-size: 13px; font-weight: 600; color: var(--text-3); }
    .field input {
      height: 38px;
      padding: 0 11px;
      border: 1px solid var(--border);
      border-radius: var(--radius);
      font: inherit;
      outline: none;
      transition: border-color .15s, box-shadow .15s;
    }
    .field input:focus {
      border-color: var(--primary);
      box-shadow: 0 0 0 2px rgba(22,119,255,.15);
    }
    .error {
      display: block;
      margin-bottom: 16px;
      padding: 8px 11px;
      background: #fff2f0;
      border: 1px solid #ffccc7;
      border-radius: var(--radius);
      color: #cf1322;
      font-size: 13px;
    }
    button[type=submit] {
      display: block;
      width: 100%;
      height: 40px;
      background: var(--primary);
      color: #fff;
      border: none;
      border-radius: var(--radius);
      font: 600 14px/1 inherit;
      cursor: pointer;
      transition: background .15s;
      margin-top: 4px;
    }
    button[type=submit]:hover { background: var(--primary-hover); }
    .footer { margin-top: 20px; text-align: center; font-size: 12px; color: var(--text-3); }
  </style>
</head>
<body>
  <div class="card">
    <div class="brand">
      <div class="logo-mark">N</div>
      <div>
        <div class="brand-name">NexusTier</div>
        <div class="brand-sub">控制台登录</div>
      </div>
    </div>
    {{if .Error}}<span class="error">{{.Error}}</span>{{end}}
    <form method="POST" action="/login" autocomplete="on">
      <div class="field">
        <label for="username">用户名</label>
        <input id="username" name="username" type="text" autocomplete="username"
               required autofocus>
      </div>
      <div class="field">
        <label for="password">密码</label>
        <input id="password" name="password" type="password" autocomplete="current-password" required>
      </div>
      <button type="submit">登录</button>
    </form>
    <p class="footer">NexusTier 内部运维控制台 · 仅限授权人员使用</p>
  </div>
</body>
</html>`))

type loginData struct {
	Error string
}

// The page ships no JavaScript, so everything but its own inline stylesheet and
// same-origin form post can be denied outright.
const loginContentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func (guard *Guard) renderLogin(writer http.ResponseWriter, status int, errorMessage string) {
	// Render first: a template failure must not append to an already-sent status.
	var buf strings.Builder
	if err := loginTemplate.Execute(&buf, loginData{Error: errorMessage}); err != nil {
		guard.logger.Error("render login page failed", "error", err)
		http.Error(writer, "internal error", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", loginContentSecurityPolicy)
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, buf.String())
}
