#!/bin/sh
# Builds the Godot Android source zip used by export_presets.cfg
# and installs it into client/android/build. Requires matching export templates.
# zip(1) is not required. The official template asks Gradle for 4536m;
# this script lowers that so a 4GB machine is not OOM-killed.
set -eu
ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TPL="${GODOT_TEMPLATES:-$HOME/.local/share/godot/export_templates/4.7.2.stable}/android_source.zip"
OUT="$ROOT/client/android/source.zip"
BUILD="$ROOT/client/android/build"
if [ ! -f "$TPL" ]; then
  echo "нет $TPL" >&2
  exit 1
fi
python3 - "$TPL" "$OUT" "$BUILD" << 'PY'
import hashlib, io, pathlib, shutil, sys, zipfile
src, dst, build = sys.argv[1:]
dst_path = pathlib.Path(dst)
dst_path.parent.mkdir(parents=True, exist_ok=True)
zin = zipfile.ZipFile(src)
buf = io.BytesIO()
with zipfile.ZipFile(buf, "w") as zout:
    for info in zin.infolist():
        raw = zin.read(info.filename)
        if info.filename == "src/main/AndroidManifest.xml":
            text = raw.decode()
            if "usesCleartextTraffic" not in text:
                text = text.replace(
                    "<application",
                    '<application\n        android:usesCleartextTraffic="true"',
                    1,
                )
            raw = text.encode()
        elif info.filename == "gradle.properties":
            text = raw.decode()
            text = text.replace(
                "org.gradle.jvmargs=-Xmx4536m",
                "org.gradle.jvmargs=-Xmx1024m -XX:MaxMetaspaceSize=256m -XX:+UseSerialGC -Dfile.encoding=UTF-8\norg.gradle.workers.max=2\norg.gradle.parallel=false",
            )
            raw = text.encode()
        zout.writestr(info, raw)
data = buf.getvalue()
dst_path.write_bytes(data)
digest = hashlib.md5(data).hexdigest()
build_path = pathlib.Path(build)
if build_path.exists():
    shutil.rmtree(build_path)
build_path.mkdir(parents=True)
with zipfile.ZipFile(io.BytesIO(data)) as ready:
    ready.extractall(build_path)
gradlew = build_path / "gradlew"
if gradlew.exists():
    gradlew.chmod(0o755)
(build_path / ".gdignore").write_text("")
ident = f"res://android/source.zip [{digest}]\n"
(build_path.parent / ".build_version").write_text(ident)
print("template", dst)
print("version", ident.strip())
PY
