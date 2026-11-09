import os
import fnmatch

OUTPUT_FILE = "project_bundle.txt"

EXCLUDE_DIRS = {".git", ".idea", ".vscode", "node_modules", "venv", "__pycache__", "dist", "build", "vendor", ".opencode"}
EXCLUDE_EXTENSIONS = {".png", ".jpg", ".jpeg", ".gif", ".ico", ".pdf", ".zip", ".exe", ".dll", ".pyc", ".lock"}
EXCLUDE_FILES = {OUTPUT_FILE, "package-lock.json", "yarn.lock", "poetry.lock"}

def load_gitignore():
    patterns = []
    if os.path.exists(".gitignore"):
        with open(".gitignore", "r", encoding="utf-8", errors="ignore") as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#"):
                    patterns.append(line)
    return patterns

def should_ignore(path, root_dir, gitignore_patterns):
    rel_path = os.path.relpath(path, root_dir)
    parts = rel_path.split(os.sep)
    if any(p in EXCLUDE_DIRS for p in parts):
        return True
    
    filename = os.path.basename(path)
    if filename in EXCLUDE_FILES:
        return True
        
    _, ext = os.path.splitext(filename)
    if ext.lower() in EXCLUDE_EXTENSIONS:
        return True

    for pattern in gitignore_patterns:
        if fnmatch.fnmatch(filename, pattern) or fnmatch.fnmatch(rel_path, pattern):
            return True
            
    return False

def generate_tree(root_dir, gitignore_patterns):
    """Генерация текстового дерева проекта."""
    tree_lines = []
    tree_lines.append("PROJECT STRUCTURE:")
    tree_lines.append("=" * 50)
    
    for root, dirs, files in os.walk(root_dir):
        # Фильтруем списки "на лету"
        dirs[:] = sorted([d for d in dirs if not should_ignore(os.path.join(root, d), root_dir, gitignore_patterns)])
        files = sorted([f for f in files if not should_ignore(os.path.join(root, f), root_dir, gitignore_patterns)])
        
        rel_path = os.path.relpath(root, root_dir)
        if rel_path == ".":
            level = 0
            indent = ""
            folder_name = os.path.basename(os.path.abspath(root_dir))
            tree_lines.append(f"{folder_name}/")
        else:
            level = rel_path.count(os.sep) + 1
            indent = "    " * (level - 1) + "├── "
            tree_lines.append(f"{indent}{os.path.basename(root)}/")
        
        sub_indent = "    " * level + "├── "
        for i, file in enumerate(files):
            # Для последнего элемента ветки можно использовать └──, но для простоты оставим ├──
            tree_lines.append(f"{sub_indent}{file}")
            
    tree_lines.append("=" * 50 + "\n")
    return "\n".join(tree_lines)

def bundle_project():
    root_dir = "."
    gitignore_patterns = load_gitignore()
    
    # 1. Генерируем дерево
    tree_content = generate_tree(root_dir, gitignore_patterns)
    
    # 2. Собираем файлы и записываем всё в итоговый файл
    with open(OUTPUT_FILE, "w", encoding="utf-8") as outfile:
        # Сначала пишем дерево
        outfile.write(tree_content)
        outfile.write("\nFILE CONTENTS:\n")
        
        for root, dirs, files in os.walk(root_dir):
            dirs[:] = [d for d in dirs if not should_ignore(os.path.join(root, d), root_dir, gitignore_patterns)]
            
            for file in files:
                file_path = os.path.join(root, file)
                if should_ignore(file_path, root_dir, gitignore_patterns):
                    continue
                
                relative_path = os.path.relpath(file_path, root_dir)
                
                outfile.write(f"\n{'='*50}\n")
                outfile.write(f"FILE: {relative_path}\n")
                outfile.write(f"{'='*50}\n\n")
                
                try:
                    with open(file_path, "r", encoding="utf-8", errors="ignore") as infile:
                        outfile.write(infile.read())
                    outfile.write("\n")
                except Exception as e:
                    outfile.write(f"[Ошибка чтения файла: {e}]\n")

    print(f"Готово! Дерево и файлы собраны в {OUTPUT_FILE}")

if __name__ == "__main__":
    bundle_project()
