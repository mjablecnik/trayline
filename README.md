# Instalace

## 1. Build image
docker build -t kiro-sandbox .

## 2. Přihlaš se na hostiteli (jednorázově)
Nainstaluj Kiro CLI přímo ve WSL a přihlaš se — credentials se pak namountují do kontejneru.

```bash
curl -fsSL https://kiro.dev/install.sh | bash
kiro-cli login
```

## 3. Spusť s projektem
./run.sh ~/muj-projekt "Přidej endpoint /health do main.go"
