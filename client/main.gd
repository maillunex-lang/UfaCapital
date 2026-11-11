extends Control

const DEFAULT_BASE := "http://127.0.0.1:8088"

func _ready() -> void:
	var base := OS.get_environment("UFACAPITAL_API")
	if base.is_empty():
		base = DEFAULT_BASE
	%Status.text = "Деловая республика. Сервер: %s" % base
	%HTTP.request_completed.connect(_on_health)
	var err: int = %HTTP.request(base + "/healthz")
	if err != OK:
		%Status.text = "HTTPRequest не запущен: %s" % err

func _on_health(result: int, code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		%Status.text += "\nСервер не ответил (%s). Мир считается только на сервере." % result
		return
	%Status.text += "\nОтвет /healthz: HTTP %s\n%s" % [code, body.get_string_from_utf8()]
