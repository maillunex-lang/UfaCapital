extends Control

const SAVE_PATH := "user://session.cfg"

var http: HTTPRequest
var base := ""
var access := ""
var refresh_token := ""
var company_id := ""
var enterprise_id := ""
var queue: Array = []
var inflight := false

var url_edit: LineEdit
var email_edit: LineEdit
var pass_edit: LineEdit
var name_edit: LineEdit
var company_edit: LineEdit
var status: Label
var cash_label: Label
var tick_label: Label

func _ready() -> void:
	_load_session()
	_build()
	http = HTTPRequest.new()
	http.timeout = 25
	add_child(http)
	http.request_completed.connect(_on_http)
	_say("Укажите адрес сервера. 127.0.0.1 на телефоне — это сам телефон, не компьютер.")

func _build() -> void:
	var city := TextureRect.new()
	city.texture = load("res://art/city.jpg")
	city.expand_mode = TextureRect.EXPAND_IGNORE_SIZE
	city.stretch_mode = TextureRect.STRETCH_KEEP_ASPECT_COVERED
	city.mouse_filter = Control.MOUSE_FILTER_IGNORE
	city.set_anchors_preset(Control.PRESET_FULL_RECT)
	add_child(city)
	var shade := ColorRect.new()
	shade.color = Color(0.08, 0.1, 0.09, 0.18)
	shade.mouse_filter = Control.MOUSE_FILTER_IGNORE
	shade.set_anchors_preset(Control.PRESET_FULL_RECT)
	add_child(shade)
	var goods: Texture2D = load("res://art/goods.jpg")
	_hotspot(0.04, 0.16, 0.28, 0.58, "Мельница", func() -> void: _open("mill"))
	_hotspot(0.34, 0.12, 0.58, 0.56, "Пекарня", func() -> void: _open("bakery"))
	_hotspot(0.60, 0.18, 0.78, 0.58, "Склад", func() -> void: _open("warehouse"))
	_hotspot(0.79, 0.28, 0.97, 0.62, "Лавка", func() -> void: _open("shop"))
	var top := PanelContainer.new()
	top.add_theme_stylebox_override("panel", _glass(Color(0.11, 0.13, 0.12, 0.82)))
	top.set_anchors_preset(Control.PRESET_TOP_WIDE)
	top.offset_bottom = 92
	add_child(top)
	var head := HBoxContainer.new()
	head.add_theme_constant_override("separation", 16)
	top.add_child(head)
	var mark := TextureRect.new()
	mark.texture = _piece(goods, Rect2(760, 760, 560, 560))
	mark.custom_minimum_size = Vector2(72, 72)
	mark.expand_mode = TextureRect.EXPAND_IGNORE_SIZE
	mark.stretch_mode = TextureRect.STRETCH_KEEP_ASPECT_CENTERED
	head.add_child(mark)
	var title := Label.new()
	title.text = "UFACAPITAL\nДеловая республика · Уфа"
	title.add_theme_font_size_override("font_size", 28)
	title.add_theme_color_override("font_color", Color("f3efe4"))
	title.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	head.add_child(title)
	tick_label = Label.new()
	tick_label.add_theme_font_size_override("font_size", 22)
	tick_label.add_theme_color_override("font_color", Color("f3efe4"))
	head.add_child(tick_label)
	cash_label = Label.new()
	cash_label.add_theme_font_size_override("font_size", 28)
	cash_label.add_theme_color_override("font_color", Color("e7c56a"))
	head.add_child(cash_label)
	for piece in [[Rect2(70, 70, 560, 560), "зерно"], [Rect2(790, 70, 540, 560), "мука"], [Rect2(80, 760, 560, 560), "хлеб"]]:
		var icon := TextureRect.new()
		icon.texture = _piece(goods, piece[0])
		icon.custom_minimum_size = Vector2(64, 64)
		icon.expand_mode = TextureRect.EXPAND_IGNORE_SIZE
		icon.stretch_mode = TextureRect.STRETCH_KEEP_ASPECT_CENTERED
		icon.tooltip_text = piece[1]
		head.add_child(icon)
	var dock := PanelContainer.new()
	dock.add_theme_stylebox_override("panel", _glass(Color(0.96, 0.93, 0.86, 0.92)))
	dock.set_anchors_preset(Control.PRESET_BOTTOM_WIDE)
	dock.offset_top = -248
	add_child(dock)
	var root := VBoxContainer.new()
	root.add_theme_constant_override("separation", 8)
	dock.add_child(root)
	url_edit = _field("Адрес сервера, например http://78.17.214.25:8088")
	url_edit.text = base
	root.add_child(url_edit)
	var auth := HBoxContainer.new()
	auth.add_theme_constant_override("separation", 8)
	root.add_child(auth)
	email_edit = _field("почта")
	pass_edit = _field("пароль от 12 символов")
	pass_edit.secret = true
	name_edit = _field("имя")
	company_edit = _field("название компании")
	for box in [email_edit, pass_edit, name_edit, company_edit]:
		box.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		auth.add_child(box)
	var actions := HBoxContainer.new()
	actions.add_theme_constant_override("separation", 8)
	root.add_child(actions)
	_button(actions, "Войти в город", _register)
	_button(actions, "Войти", _login)
	_button(actions, "Выйти", _logout)
	_button(actions, "Компания", _company)
	_button(actions, "Зерно ×10", _buy_grain)
	_button(actions, "Пуск", func() -> void: _policy(true))
	_button(actions, "Стоп", func() -> void: _policy(false))
	_button(actions, "Обновить", _refresh)
	status = Label.new()
	status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	status.size_flags_vertical = Control.SIZE_EXPAND_FILL
	status.add_theme_color_override("font_color", Color("1c2421"))
	status.add_theme_font_size_override("font_size", 18)
	root.add_child(status)

func _glass(color: Color) -> StyleBoxFlat:
	var box := StyleBoxFlat.new()
	box.bg_color = color
	box.content_margin_left = 14
	box.content_margin_right = 14
	box.content_margin_top = 10
	box.content_margin_bottom = 10
	return box

func _piece(atlas: Texture2D, region: Rect2) -> AtlasTexture:
	var piece := AtlasTexture.new()
	piece.atlas = atlas
	piece.region = region
	return piece

func _hotspot(x1: float, y1: float, x2: float, y2: float, caption: String, cb: Callable) -> void:
	var b := Button.new()
	b.text = caption
	b.anchor_left = x1
	b.anchor_top = y1
	b.anchor_right = x2
	b.anchor_bottom = y2
	b.offset_left = 0
	b.offset_top = 0
	b.offset_right = 0
	b.offset_bottom = 0
	b.add_theme_font_size_override("font_size", 22)
	b.add_theme_color_override("font_color", Color("f6f1e4"))
	b.add_theme_color_override("font_hover_color", Color("1c2421"))
	var normal := StyleBoxFlat.new()
	normal.bg_color = Color(0.09, 0.12, 0.1, 0.28)
	normal.border_color = Color(0.95, 0.86, 0.62, 0.9)
	normal.set_border_width_all(2)
	normal.set_corner_radius_all(10)
	var hover := normal.duplicate() as StyleBoxFlat
	hover.bg_color = Color(0.91, 0.78, 0.42, 0.55)
	b.add_theme_stylebox_override("normal", normal)
	b.add_theme_stylebox_override("hover", hover)
	b.add_theme_stylebox_override("pressed", hover)
	b.alignment = HORIZONTAL_ALIGNMENT_CENTER
	b.pressed.connect(cb)
	add_child(b)

func _field(placeholder: String) -> LineEdit:
	var edit := LineEdit.new()
	edit.placeholder_text = placeholder
	edit.custom_minimum_size = Vector2(0, 48)
	return edit

func _button(parent: Node, caption: String, cb: Callable) -> void:
	var b := Button.new()
	b.text = caption
	b.custom_minimum_size = Vector2(0, 48)
	b.pressed.connect(cb)
	parent.add_child(b)

func _say(text: String) -> void:
	if status:
		status.text = text

func _base() -> String:
	return url_edit.text.strip_edges().trim_suffix("/")

func _save() -> void:
	var cfg := ConfigFile.new()
	cfg.set_value("session", "base", _base())
	cfg.set_value("session", "access", access)
	cfg.set_value("session", "refresh", refresh_token)
	cfg.save(SAVE_PATH)

func _load_session() -> void:
	var cfg := ConfigFile.new()
	if cfg.load(SAVE_PATH) != OK:
		return
	base = str(cfg.get_value("session", "base", ""))
	access = str(cfg.get_value("session", "access", ""))
	refresh_token = str(cfg.get_value("session", "refresh", ""))

func _enqueue(path: String, method: int, body: String, authed: bool, tag := "") -> void:
	var headers: PackedStringArray = PackedStringArray()
	headers.append("Content-Type: application/json")
	headers.append("Idempotency-Key: %s" % _uuid())
	if authed and access != "":
		headers.append("Authorization: Bearer %s" % access)
	queue.append({
		"path": path, "method": method, "body": body, "headers": headers,
		"retried": false, "tag": tag,
	})
	_pump()

func _pump() -> void:
	if inflight or queue.is_empty():
		return
	var job: Dictionary = queue[0]
	if _base() == "":
		_say("Сначала укажите адрес сервера.")
		queue.clear()
		return
	inflight = true
	var err := http.request(_base() + str(job["path"]), job["headers"], job["method"], str(job["body"]))
	if err != OK:
		inflight = false
		queue.pop_front()
		_say("Запрос не ушёл: %s" % err)

func _on_http(result: int, code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	inflight = false
	if queue.is_empty():
		return
	var job: Dictionary = queue.pop_front()
	var text := body.get_string_from_utf8()
	if result != HTTPRequest.RESULT_SUCCESS:
		_say("Сеть недоступна (%s). Проверьте адрес и что сервер открыт для телефона." % result)
		return
	if code == 401 and not bool(job["retried"]) and refresh_token != "":
		job["retried"] = true
		queue.push_front(job)
		queue.push_front({
			"path": "/api/v1/auth/refresh",
			"method": HTTPClient.METHOD_POST,
			"body": JSON.stringify({"refresh_token": refresh_token}),
			"headers": PackedStringArray(["Content-Type: application/json", "Idempotency-Key: %s" % _uuid()]),
			"retried": false,
			"tag": "refresh",
		})
		_pump()
		return
	var parsed = JSON.parse_string(text)
	var data := {}
	if typeof(parsed) == TYPE_DICTIONARY:
		data = parsed
	if code >= 400:
		var message := text
		if data.has("error") and typeof(data["error"]) == TYPE_DICTIONARY:
			message = str(data["error"].get("message", text))
		_say("Ошибка %s: %s" % [code, message])
		_pump()
		return
	var payload = data.get("data", {})
	var path := str(job["path"])
	var tag := str(job["tag"])
	if tag == "refresh" or path.ends_with("/auth/register") or path.ends_with("/auth/login"):
		if typeof(payload) == TYPE_DICTIONARY:
			access = str(payload.get("access_token", access))
			refresh_token = str(payload.get("refresh_token", refresh_token))
			_save()
			if tag != "refresh":
				_say("Сессия открыта.")
				_refresh()
				return
	if tag == "company" and typeof(payload) == TYPE_DICTIONARY:
		company_id = str(payload.get("company_id", company_id))
		_say("Компания создана. Грант %s." % str(payload.get("grant", "")))
		_refresh()
		return
	if tag == "board" and typeof(payload) == TYPE_DICTIONARY:
		_show_board(payload)
		return
	if tag == "market" and typeof(payload) == TYPE_ARRAY:
		_accept_grain(payload)
		return
	if tag == "open" and typeof(payload) == TYPE_DICTIONARY:
		enterprise_id = str(payload.get("enterprise_id", enterprise_id))
		_say("Предприятие открыто. Нажмите «Пуск», когда есть сырьё и штат.")
		_hire()
		return
	_say("Готово.")
	_pump()

func _show_board(board: Dictionary) -> void:
	tick_label.text = "  тик %s" % str(board.get("tick", "0"))
	var company = board.get("company", null)
	if typeof(company) != TYPE_DICTIONARY:
		cash_label.text = ""
		company_id = ""
		_say("Компании ещё нет. Придумайте название и нажмите «Компания».")
		_pump()
		return
	company_id = str(company.get("id", ""))
	cash_label.text = "  %s ₽" % str(company.get("cash", "0"))
	var lines: PackedStringArray = PackedStringArray()
	lines.append("%s · %s · касса %s, резерв %s" % [
		company.get("name", ""), company.get("owner_kind", ""),
		company.get("cash", ""), company.get("reserved", ""),
	])
	var stock = board.get("stock", [])
	if typeof(stock) == TYPE_ARRAY and stock.is_empty():
		lines.append("Склад пуст.")
	elif typeof(stock) == TYPE_ARRAY:
		for row in stock:
			if typeof(row) == TYPE_DICTIONARY:
				lines.append("%s: %s, в резерве %s" % [row.get("good", ""), row.get("qty", ""), row.get("reserved", "")])
	var enterprises = board.get("enterprises", [])
	enterprise_id = ""
	if typeof(enterprises) == TYPE_ARRAY:
		for row in enterprises:
			if typeof(row) != TYPE_DICTIONARY:
				continue
			var template := str(row.get("template", ""))
			if template == "mill" or template == "bakery":
				enterprise_id = str(row.get("id", enterprise_id))
			lines.append("%s · %s · штат %s" % [template, row.get("status", ""), row.get("headcount", "")])
	var peers = board.get("peers", [])
	if typeof(peers) == TYPE_ARRAY:
		var names: PackedStringArray = PackedStringArray()
		for row in peers:
			if typeof(row) == TYPE_DICTIONARY and bool(row.get("is_ai", false)):
				names.append(str(row.get("name", "")))
		if names.size() > 0:
			lines.append("ИИ на рынке: %s" % ", ".join(names))
	status.text = "\n".join(lines)
	_pump()

func _accept_grain(rows: Array) -> void:
	for row in rows:
		if typeof(row) != TYPE_DICTIONARY:
			continue
		if str(row.get("good", "")) == "grain" and str(row.get("side", "")) == "sell":
			var price := int(str(row.get("price", "0")))
			_enqueue("/api/v1/market/orders/%s/accept" % str(row.get("order_id", "")), HTTPClient.METHOD_POST, JSON.stringify({
				"qty": "10",
				"max_total": str(price * 10),
			}), true, "buy")
			return
	_say("Нет открытого предложения зерна.")
	_pump()

func _uuid() -> String:
	return "%s-%s" % [Time.get_unix_time_from_system(), randi()]

func _register() -> void:
	_save()
	_enqueue("/api/v1/auth/register", HTTPClient.METHOD_POST, JSON.stringify({
		"email": email_edit.text.strip_edges(),
		"password": pass_edit.text,
		"display_name": name_edit.text.strip_edges(),
	}), false)

func _login() -> void:
	_save()
	_enqueue("/api/v1/auth/login", HTTPClient.METHOD_POST, JSON.stringify({
		"email": email_edit.text.strip_edges(),
		"password": pass_edit.text,
	}), false)

func _logout() -> void:
	access = ""
	refresh_token = ""
	company_id = ""
	_save()
	cash_label.text = ""
	_say("Сессия закрыта на этом телефоне.")

func _company() -> void:
	var title := company_edit.text.strip_edges()
	if title == "":
		title = name_edit.text.strip_edges()
	_enqueue("/api/v1/companies", HTTPClient.METHOD_POST, JSON.stringify({"name": title}), true, "company")

func _refresh() -> void:
	_enqueue("/api/v1/companies/board", HTTPClient.METHOD_GET, "", true, "board")

func _buy_grain() -> void:
	_enqueue("/api/v1/market", HTTPClient.METHOD_GET, "", true, "market")

func _open(template: String) -> void:
	if company_id == "":
		_say("Сначала создайте компанию.")
		return
	_enqueue("/api/v1/enterprises", HTTPClient.METHOD_POST, JSON.stringify({
		"company_id": company_id,
		"template": template,
	}), true, "open")

func _hire() -> void:
	if enterprise_id == "":
		return
	_enqueue("/api/v1/enterprises/%s/workforce" % enterprise_id, HTTPClient.METHOD_POST, JSON.stringify({
		"headcount": "2",
		"wage": "15",
	}), true, "hire")

func _policy(running: bool) -> void:
	if enterprise_id == "":
		_say("Сначала откройте мельницу или пекарню.")
		return
	_enqueue("/api/v1/enterprises/%s/policy" % enterprise_id, HTTPClient.METHOD_POST, JSON.stringify({
		"running": running,
	}), true, "policy")
