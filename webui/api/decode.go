package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

// maxJSONBodyBytes 普通 JSON 请求体上限（1 MiB）。
const maxJSONBodyBytes = 1 << 20

// maxImportBodyBytes 配置导入请求体上限（10 MiB）。
//
// 导入与普通 API 共用同一严格解码语义，只有大小上限不同（Build6 §12.8、§3.2）。
const maxImportBodyBytes = 10 << 20

// httpError 已分类的 4xx 请求错误：状态码 + 安全文案。
//
// 只用于 handler 明确构造的请求侧错误；底层数据库或内部错误不得包装成
// httpError，否则会把内部原因回显给客户端。
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

// badRequest 构造 400 错误
func badRequest(msg string) error { return &httpError{status: http.StatusBadRequest, msg: msg} }

// notFound 构造 404 错误
func notFound(msg string) error { return &httpError{status: http.StatusNotFound, msg: msg} }

// conflict 构造 409 错误
func conflict(msg string) error { return &httpError{status: http.StatusConflict, msg: msg} }

// decodeJSONStrict 严格解码普通 JSON 请求体（Build6 §12.8、§4.1）：
//
//   - body 超过 limit 返回 413；
//   - 拒绝未知字段、尾随 JSON 与多个顶层值；
//   - 语法错误、类型错误与空 body 返回 400。
//
// 错误文案只包含字段路径与原因，不回显请求体内容。
func decodeJSONStrict(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}

	// 只允许一个顶层 JSON 值：第二次解码必须直接得到 io.EOF
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return badRequest("请求体只能包含一个 JSON 值")
		}
		return decodeError(err)
	}
	return nil
}

// decodeError 把 json 解码错误映射为带状态码的安全错误。
func decodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &httpError{status: http.StatusRequestEntityTooLarge, msg: "请求体超过大小上限"}
	}
	if errors.Is(err, io.EOF) {
		return badRequest("请求体不能为空")
	}
	if name, ok := unknownFieldName(err); ok {
		return badRequest("请求体包含未知字段: " + name)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field != "" {
			return badRequest("字段类型错误: " + typeErr.Field)
		}
		return badRequest("字段类型错误")
	}
	// 语法错误不回显原始字符
	return badRequest("请求体 JSON 格式错误")
}

// unknownFieldName 从 DisallowUnknownFields 的错误中取出未知字段名。
//
// encoding/json 对该错误只提供文本（形如 `json: unknown field "id"`），
// 因此这里做前缀匹配并去掉引号；字段名本身不敏感，可以返回给客户端。
func unknownFieldName(err error) (string, bool) {
	const prefix = "json: unknown field "
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(msg, prefix)
	if unquoted, uerr := strconv.Unquote(name); uerr == nil {
		return unquoted, true
	}
	return name, true
}

// parsePathID 严格解析路径参数 id：必须是十进制整数且大于 0（Build6 §4.1）。
//
// 不使用会接受 `12abc`、`-5` 这类前缀数字的宽松扫描。
func parsePathID(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || n <= 0 {
		return 0, badRequest("路径 ID 必须是大于 0 的十进制整数")
	}
	return n, nil
}

// clientError 提取已分类的 4xx 错误（httpError 或 config 领域校验错误）。
// 未分类时返回 nil，由调用方按请求错误或内部错误处理。
func clientError(err error) *httpError {
	var he *httpError
	if errors.As(err, &he) {
		return he
	}
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return &httpError{status: http.StatusBadRequest, msg: ve.Error()}
	}
	return nil
}

// writeRequestError 写出请求侧错误：已分类错误按其状态码，其余按 400。
func writeRequestError(w http.ResponseWriter, err error) {
	if he := clientError(err); he != nil {
		writeError(w, he.status, he.msg)
		return
	}
	writeError(w, http.StatusBadRequest, "请求无效")
}

// writeMutationError 写出配置变更事务返回的错误：
// 已分类的 4xx 错误按原因返回，其余按内部错误处理（安全文案 + 服务端日志）。
func writeMutationError(w http.ResponseWriter, err error) {
	if he := clientError(err); he != nil {
		writeError(w, he.status, he.msg)
		return
	}
	writeInternalError(w, "保存失败", err)
}

// writeInternalError 记录真实错误，但只向客户端返回安全通用文案（Build6 §12.8）。
//
// 真实 error 只写服务日志，便于排查；不得包含请求 body 或敏感字段值。
func writeInternalError(w http.ResponseWriter, msg string, err error) {
	slog.Error(msg, "error", err)
	writeError(w, http.StatusInternalServerError, msg)
}
