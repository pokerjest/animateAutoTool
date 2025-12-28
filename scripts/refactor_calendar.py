
import os

path = '/Users/pokerjest/github/animateAutoTool/web/templates/calendar.html'

# 1. Read the file
with open(path, 'r') as f:
    lines = f.readlines()

# 2. Find the x-data block range (approx lines 4 to 180)
# Line 4 (idx 3) starts with <div ... x-data="{
# Line 180 (idx 179) matches "}">
start_idx = 3
end_idx = 179
# Check content to be safe
if 'x-data="{' in lines[start_idx] and '}"' in lines[end_idx]:
    print(f"Found x-data block matching expectation: {start_idx+1} to {end_idx+1}")
else:
    # Scan for it
    start_idx = -1
    end_idx = -1
    for i, line in enumerate(lines):
        if 'x-data="{' in line:
            start_idx = i
        if start_idx != -1 and line.strip() == '}">':
            end_idx = i
            break
    print(f"Scanned x-data block: {start_idx+1} to {end_idx+1}")

if start_idx == -1 or end_idx == -1:
    print("Could not locate x-data block. Aborting.")
    exit(1)

# 3. Extract logic
x_data_content = lines[start_idx+1:end_idx] # Lines inside { ... }
# We need to preserve the opening `{` content if any? No, line 4 is `x-data="{`.
# The logic inside is `key: value,`.
# We want to wrap it in `Alpine.data('calendarData', () => ({ ... }))`

# 4. Construct Script
# Join the lines
logic_body = "".join(x_data_content)

# Fix the broken previewRSS string if present (the newline issue)
logic_body = logic_body.replace("previewContainer.innerHTML='<div class=\\' p-8", "previewContainer.innerHTML='<div class=\"p-8")
logic_body = logic_body.replace("gap-2\\'>", "gap-2\">")
logic_body = logic_body.replace("rounded-full\\'></div>", "rounded-full\"></div>")
# Remove user's previous failed escaping attempts and just use backticks for safety
# Actually, since we are in a script tag, we can use backticks!
# Replace the entire previewRSS function body if possible, or just fix the quotes.
# I'll rely on the simple quote fix for now, or replace specific broken strings.
# Wait, the CURRENT file has BROKEN content (newlines in string). I MUST fix it.

# I will replace the broken previewRSS block with a clean backtick version.
broken_rss_start = "async previewRSS() {"
rss_fix = """
         async previewRSS() {
            const rssInput = document.getElementById('calendar-rss-url');
            const url = rssInput ? rssInput.value : '';
            if (!url) return;
            
            const previewContainer = document.getElementById('calendar-preview-results');
            if(!previewContainer) return;
            
            previewContainer.innerHTML = `<div class="p-8 text-center text-gray-400 flex flex-col items-center gap-2">
                <div class="animate-spin h-6 w-6 border-2 border-pink-500 border-t-transparent rounded-full"></div>
                <span>正在解析预览...</span>
            </div>`;
            
            try {
                const resp = await fetch('/api/preview?RSSUrl=' + encodeURIComponent(url));
                if (resp.ok) {
                    const html = await resp.text();
                    previewContainer.innerHTML = html;
                } else {
                    previewContainer.innerHTML = `<div class="p-4 text-center text-red-500 bg-red-50 rounded-lg">解析失败: ${resp.statusText}</div>`;
                }
            } catch(e) {
                 previewContainer.innerHTML = `<div class="p-4 text-center text-red-500 bg-red-50 rounded-lg">网络错误: ${e.message}</div>`;
            }
         },
"""

# Helper to find reference and replace
if broken_rss_start in logic_body:
    # Find end of function? It's roughly indented.
    # Simpler: Find the range in the original lines and replace in list before joining
    pass

script_content = f"""
<script>
    document.addEventListener('alpine:init', () => {{
        Alpine.data('calendarData', () => ({{
{logic_body}
        }}));
    }});
</script>
"""

# Apply the RSS fix to script_content directly (string manipulation)
# Finding the start of previewRSS in script_content
start_rss = script_content.find("async previewRSS() {")
if start_rss != -1:
    # Find the closing brace of the function.
    # Since indentation is reliable-ish?
    # I'll just search for the next function "async playEpisode" and slice before it
    end_rss = script_content.find("async playEpisode", start_rss)
    if end_rss != -1:
        # Check for comma before playEpisode
        # Look back from end_rss
        # It's usually `},` before `async playEpisode`
        # I'll replace the range [start_rss : end_rss] with rss_fix
        # But wait, rss_fix doesn't have the trailing comma? I need to be careful.
        # Let's see rss_fix. It ends with `},`.
        # The block I'm replacing includes `},`?
        # The original code between `previewRSS` and `playEpisode` usually has `},`
        original_chunk = script_content[start_rss:end_rss]
        # I'll just replace `original_chunk` with `rss_fix` (ensuring I preserve the comma if needed)
        # rss_fix has `},`. Logic body items are comma separated.
        # So `rss_fix` should represent the whole function value.
        # I'll strip the leading/trailing whitespace from rss_fix to match.
        
        # Actually, best to just replace the broken string parts if I can't guarantee structure.
        # But the broken string has NEWLINES which valid JS strings don't support (unless backtick).
        # Switching to backticks is the Fix.
        pass

# 5. Modify lines
# Replace start line
lines[start_idx] = lines[start_idx].replace('x-data="{', 'x-data="calendarData"')
# Remove lines between start and end
del lines[start_idx+1:end_idx+1] # Remove content + closing line
# Insert Script at end
# Find {{ end }}
last_end_idx = -1
for i in range(len(lines)-1, -1, -1):
    if '{{ end }}' in lines[i]:
        last_end_idx = i
        break

if last_end_idx != -1:
    lines.insert(last_end_idx, script_content)
else:
    lines.append(script_content)

# 6. Apply string replacements to the WHOLE file content relative to the script
final_content = "".join(lines)

# Apply that RSS fix carefully
# The logic_body we extracted earlier had invalid strings.
# I will use a regex or direct replacement on `final_content`.
import re
# Regex to match the broken previewRSS function body roughly?
# It contains `previewContainer.innerHTML='<div class=\' p-8`
# I'll replace the quoted strings that are broken.
final_content = final_content.replace(
    "previewContainer.innerHTML='<div class=\\' p-8 text-center text-gray-400 flex flex-col items-center gap-2\\'>\\n    <div class=\\'animate-spin h-6 w-6 border-2 border-pink-500 border-t-transparent rounded-full\\'></div>\\n    <span>正在解析预览...</span>\\n</div>';", 
    "previewContainer.innerHTML=`<div class=\"p-8 text-center text-gray-400 flex flex-col items-center gap-2\"><div class=\"animate-spin h-6 w-6 border-2 border-pink-500 border-t-transparent rounded-full\"></div><span>正在解析预览...</span></div>`;"
)
# Also fix the catch block and else block
final_content = final_content.replace(
    "previewContainer.innerHTML = '<div class=\\'p-4 text-center text-red-500 bg-red-50 rounded-lg\\'>解析失败: ' + resp.statusText\\n    + '</div>';",
    "previewContainer.innerHTML = `<div class=\"p-4 text-center text-red-500 bg-red-50 rounded-lg\">解析失败: ${resp.statusText}</div>`;"
)
final_content = final_content.replace(
    "previewContainer.innerHTML = '<div class=\\'p-4 text-center text-red-500 bg-red-50 rounded-lg\\'>网络错误: ' + e.message + '\\n</div>';",
    "previewContainer.innerHTML = `<div class=\"p-4 text-center text-red-500 bg-red-50 rounded-lg\">网络错误: ${e.message}</div>`;"
)

# 7. Write back
with open(path, 'w') as f:
    f.write(final_content)

print("Refactor complete")
