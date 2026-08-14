path = r"D:\Fairy\frontend\server.cjs"
with open(path, "r", encoding="utf-8") as f:
    c = f.read()

start = c.find("const MODELS = {")
end = c.find("};", start) + 2
assert start >= 0 and end > start
new_models = '''const MODELS = {
  "glm-4.7-flash": { display: "GLM-4.7-Flash", kind: "real", config: path.join(REPO, "config", "config.glm.json"), override: null },
  "deepseek-chat": { display: "DeepSeek-Chat", kind: "real", config: path.join(REPO, "config", "config.deepseek.json"), override: null },
};'''
c = c[:start] + new_models + c[end:]

with open(path, "w", encoding="utf-8") as f:
    f.write(c)
print("server.cjs MODELS updated")
