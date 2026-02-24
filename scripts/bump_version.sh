#!/bin/bash
# 自动更新 WebCasa 版本号的脚本

set -e

# 获取脚本所在目录并进入项目根目录
cd "$(dirname "$0")/.."

# 检查是否提供了版本号
if [ -z "$1" ]; then
    echo "错误: 请提供一个新的版本号。"
    echo "用法: $0 <new_version>"
    echo "示例: $0 0.5.1"
    exit 1
fi

NEW_VERSION=$1

# 检查版本号格式 (基本数字和点的组合)
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
    echo "错误: 版本号格式不正确，建议使用语义化版本 (例如: 1.2.3 或 1.2.3-rc1)。"
    exit 1
fi

OLD_VERSION=$(cat VERSION | tr -d '[:space:]')

echo "当前版本: $OLD_VERSION"
echo "目标版本: $NEW_VERSION"
echo "正在修改..."

# 1. 更新 /VERSION (真相源)
echo "$NEW_VERSION" > VERSION
echo "✅ 更新 VERSION 文件"

# 2. 更新 web/package.json 和 web/package-lock.json
cd web
npm version "$NEW_VERSION" --no-git-tag-version
cd ..
echo "✅ 更新 web/package.json 和 package-lock.json"

# 3. 更新 install.sh 中的 fallback 版本号
# 使用 sed 替换 install.sh 中的版本号 fallback
sed -i "s/|| echo \"$OLD_VERSION\")/|| echo \"$NEW_VERSION\")/g" install.sh
sed -i "s/WEBCASA_VERSION=\"$OLD_VERSION\"/WEBCASA_VERSION=\"$NEW_VERSION\"/g" install.sh
echo "✅ 更新 install.sh 中的 fallback 版本"

# 4. 更新 memory.md 中的状态记录
sed -i "s/- \*\*版本\*\*: \`$OLD_VERSION\`/- \*\*版本\*\*: \`$NEW_VERSION\`/g" memory.md
echo "✅ 更新 memory.md 中的版本记录"

echo "--------------------------------------------------"
echo "🎉 版本号已成功更新为 $NEW_VERSION"
echo "提示: 请记得手动更新 changelog.md 添加新版本的更新日志。"
echo "如果你准备好提交更改，可以运行以下命令:"
echo ""
echo "git add VERSION web/package.json web/package-lock.json install.sh memory.md"
echo "git commit -m \"chore: bump version to $NEW_VERSION\""
echo "git tag v$NEW_VERSION"
echo "git push origin main --tags"

