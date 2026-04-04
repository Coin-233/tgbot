import requests
import json

def search_saucenao(image_bytes, api_key: str):
    if not api_key:
        return -1, "未配置 STOKEN"

    url = "https://saucenao.com/search.php"
    params = {
        'api_key': api_key,
        'output_type': 2, # JSON
        'numres': 6,      # 页数
        'db': 999,        #全部库
    }
    
    files = {'file': ('image.jpg', image_bytes, 'image/jpeg')}

    try:
        resp = requests.post(url, params=params, files=files, timeout=30)
        
        if resp.status_code == 429:
            return -1, "搜索过于频繁"
        
        if resp.status_code != 200:
            return -1, f"API 错误: {resp.status_code}"

        data = resp.json()
        
        header = data.get('header', {})
        if header.get('status', 0) != 0:
            return -1, f"服务端错误: {header.get('message', '')}"

        results = data.get('results', [])
        if not results:
            return -1, "QAQ 未找到相似图片"

        parsed_results = []
        
        for res in results:
            header = res.get('header', {})
            data_info = res.get('data', {})
            similarity = header.get('similarity', '0')
            if float(similarity) < 50:
                continue
            thumbnail = header.get('thumbnail', '')
            title = data_info.get('title') or data_info.get('jp_name') or data_info.get('eng_name') or ""
            if not title and 'source' in data_info:
                title = data_info['source']
            member_name = data_info.get('member_name') or data_info.get('author_name') or ""
            ext_urls = data_info.get('ext_urls', [])
            source_url = ext_urls[0] if ext_urls else ""

            parsed_results.append({
                "similarity": similarity,
                "title": title,
                "author": member_name,
                "url": source_url,
                "thumbnail": thumbnail
            })

        if not parsed_results:
            return -1, "QAQ 结果相似度都太低了"

        return 0, parsed_results

    except Exception as e:
        print(f"SauceNAO Error: {e}")
        return -1, "搜图过程发生未知错误"