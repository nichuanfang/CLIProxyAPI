import copy
import datetime
import json
import os
import shutil
import urllib.error
import urllib.request
from typing import Any, Dict, List, Optional, Tuple

DEFAULT_CATALOG_PATH = os.path.expanduser(r"~\.codex\model-catalogs\default.json")
DEFAULT_BACKUP_DIR = os.path.expanduser(r"~\.codex\model-catalogs\backups")
DEFAULT_UPSTREAM_URL = "http://127.0.0.1:8317/v1/models"
DEFAULT_API_KEY = "sk-U6cnDx0z3xtOGLoZXSGo"


def get_catalog_path(path: Optional[str] = None) -> str:
    return os.path.abspath(path or DEFAULT_CATALOG_PATH)


def get_backup_dir(path: Optional[str] = None) -> str:
    if path:
        return os.path.abspath(path)
    catalog_path = get_catalog_path()
    return os.path.join(os.path.dirname(catalog_path), "backups")


def load_catalog(path: Optional[str] = None) -> Dict[str, Any]:
    p = get_catalog_path(path)
    if not os.path.exists(p):
        raise FileNotFoundError(f"Catalog file not found: {p}")
    with open(p, "r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict) or "models" not in data or not isinstance(data["models"], list):
        raise ValueError(f"Invalid catalog format in {p}: expected root object with 'models' array")
    return data


def create_backup(path: Optional[str] = None, backup_dir: Optional[str] = None) -> str:
    src_path = get_catalog_path(path)
    if not os.path.exists(src_path):
        raise FileNotFoundError(f"Cannot backup non-existent file: {src_path}")

    bdir = get_backup_dir(backup_dir)
    os.makedirs(bdir, exist_ok=True)

    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S_%f")
    filename = os.path.basename(src_path)
    backup_filename = f"{filename}.{timestamp}.bak"
    dst_path = os.path.join(bdir, backup_filename)

    shutil.copy2(src_path, dst_path)
    return dst_path


def list_backups(backup_dir: Optional[str] = None) -> List[Dict[str, Any]]:
    bdir = get_backup_dir(backup_dir)
    if not os.path.exists(bdir):
        return []
    results = []
    for fname in os.listdir(bdir):
        if fname.endswith(".bak"):
            fpath = os.path.join(bdir, fname)
            stat = os.stat(fpath)
            results.append({
                "filename": fname,
                "path": fpath,
                "size": stat.st_size,
                "mtime": stat.st_mtime,
                "time_str": datetime.datetime.fromtimestamp(stat.st_mtime).strftime("%Y-%m-%d %H:%M:%S")
            })
    results.sort(key=lambda x: x["mtime"], reverse=True)
    return results


def restore_backup(backup_file: str, target_path: Optional[str] = None) -> str:
    if not os.path.isabs(backup_file):
        backup_file = os.path.join(get_backup_dir(), backup_file)
    if not os.path.exists(backup_file):
        raise FileNotFoundError(f"Backup file not found: {backup_file}")

    with open(backup_file, "r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict) or "models" not in data:
        raise ValueError("Selected backup file is not a valid catalog JSON")

    target = get_catalog_path(target_path)
    if os.path.exists(target):
        create_backup(target)

    with open(target, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    return target


def save_catalog(data: Dict[str, Any], path: Optional[str] = None, do_backup: bool = True) -> str:
    if not isinstance(data, dict) or "models" not in data or not isinstance(data["models"], list):
        raise ValueError("Cannot save invalid catalog: missing 'models' array")

    p = get_catalog_path(path)
    if do_backup and os.path.exists(p):
        create_backup(p)

    parent_dir = os.path.dirname(p)
    if parent_dir and not os.path.exists(parent_dir):
        os.makedirs(parent_dir, exist_ok=True)

    tmp_path = p + ".tmp"
    with open(tmp_path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")

    shutil.move(tmp_path, p)
    return p


def fetch_upstream_models(url: str = DEFAULT_UPSTREAM_URL, api_key: str = DEFAULT_API_KEY, timeout: int = 10) -> List[Dict[str, Any]]:
    req_url = url.strip()
    if not req_url.startswith("http://") and not req_url.startswith("https://"):
        req_url = "http://" + req_url
    if not req_url.endswith("/v1/models") and not "/models" in req_url:
        req_url = req_url.rstrip("/") + "/v1/models"

    headers = {"User-Agent": "Codex-Model-Catalog-Manager/1.0"}
    if api_key and api_key.strip():
        headers["Authorization"] = f"Bearer {api_key.strip()}"

    req = urllib.request.Request(req_url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            content = response.read().decode("utf-8")
            parsed = json.loads(content)
            if isinstance(parsed, dict) and "data" in parsed and isinstance(parsed["data"], list):
                return parsed["data"]
            elif isinstance(parsed, list):
                return parsed
            else:
                raise ValueError("Unexpected response format from /v1/models: missing 'data' array")
    except urllib.error.HTTPError as e:
        err_body = e.read().decode("utf-8", errors="ignore")
        raise RuntimeError(f"HTTP error {e.code} fetching {req_url}: {e.reason} - {err_body}")
    except urllib.error.URLError as e:
        raise RuntimeError(f"Network error fetching {req_url}: {e.reason}")


def compare_models(catalog_data: Dict[str, Any], upstream_models: List[Dict[str, Any]]) -> Dict[str, Any]:
    configured_map = {m["slug"]: m for m in catalog_data.get("models", []) if "slug" in m}
    upstream_list = []
    seen_slugs = set()

    for up in upstream_models:
        slug = up.get("id") or up.get("slug")
        if not slug:
            continue
        seen_slugs.add(slug)
        is_configured = slug in configured_map
        upstream_list.append({
            "slug": slug,
            "owned_by": up.get("owned_by", ""),
            "created": up.get("created"),
            "is_enabled": is_configured,
            "current_config": configured_map.get(slug)
        })

    catalog_only = []
    for slug, m in configured_map.items():
        if slug not in seen_slugs:
            catalog_only.append(m)

    return {
        "upstream_models": upstream_list,
        "catalog_only_models": catalog_only,
        "total_configured": len(configured_map),
        "total_upstream": len(upstream_list)
    }


def get_model(catalog_data: Dict[str, Any], slug: str) -> Optional[Dict[str, Any]]:
    for m in catalog_data.get("models", []):
        if m.get("slug") == slug:
            return m
    return None


def add_model(
    catalog_data: Dict[str, Any],
    new_model_config: Dict[str, Any],
    template_slug: Optional[str] = None,
    insert_position: str = "end"
) -> Dict[str, Any]:
    slug = new_model_config.get("slug", "").strip()
    if not slug:
        raise ValueError("Model slug cannot be empty")

    existing = get_model(catalog_data, slug)
    if existing:
        raise ValueError(f"Model with slug '{slug}' already exists in catalog")

    models = catalog_data.setdefault("models", [])

    base_model = None
    if template_slug:
        base_model = get_model(catalog_data, template_slug)
        if not base_model:
            raise ValueError(f"Template model '{template_slug}' not found in catalog")
    elif models:
        base_model = models[0]

    if base_model:
        created_model = copy.deepcopy(base_model)
    else:
        created_model = {
            "slug": slug,
            "display_name": slug,
            "description": f"Model {slug}",
            "default_reasoning_level": "medium",
            "supported_reasoning_levels": [
                {"effort": "high", "description": "Greater reasoning depth for complex problems"},
                {"effort": "max", "description": "Maximum reasoning depth for the hardest problems"}
            ],
            "shell_type": "shell_command",
            "visibility": "list",
            "supported_in_api": True,
            "priority": 1000,
            "additional_speed_tiers": [],
            "service_tiers": [],
            "availability_nux": None,
            "upgrade": None,
            "include_skills_usage_instructions": True,
            "include_plugin_usage_instructions": True,
            "include_apps_usage_instructions": True,
            "default_reasoning_summary": "none",
            "support_verbosity": True,
            "default_verbosity": "low",
            "apply_patch_tool_type": "freeform",
            "web_search_tool_type": "text_and_image",
            "truncation_policy": {"mode": "tokens", "limit": 10000},
            "supports_image_detail_original": True,
            "context_window": 1048576,
            "max_context_window": 1048576,
            "effective_context_window_percent": 100,
            "experimental_supported_tools": [],
            "input_modalities": ["text", "image"],
            "supports_search_tool": True,
            "use_responses_lite": False,
            "node_repl_auto_review_required": False,
            "node_repl_disabled": False,
            "auto_compact_token_limit": 943718,
            "multi_agent_version": "v2"
        }

    created_model.update(new_model_config)
    created_model["slug"] = slug

    if "priority" not in new_model_config:
        max_priority = max([m.get("priority", 999) for m in models], default=999)
        created_model["priority"] = max_priority + 1

    if insert_position == "start":
        models.insert(0, created_model)
    else:
        models.append(created_model)

    return created_model


def update_model(
    catalog_data: Dict[str, Any],
    slug: str,
    updates: Dict[str, Any]
) -> Dict[str, Any]:
    target = get_model(catalog_data, slug)
    if not target:
        raise ValueError(f"Model '{slug}' not found in catalog")

    new_slug = updates.get("slug")
    if new_slug and new_slug != slug:
        if get_model(catalog_data, new_slug):
            raise ValueError(f"Cannot rename to '{new_slug}', slug already exists")

    for k, v in updates.items():
        target[k] = v

    return target


def remove_model(catalog_data: Dict[str, Any], slug: str) -> Dict[str, Any]:
    models = catalog_data.get("models", [])
    for i, m in enumerate(models):
        if m.get("slug") == slug:
            removed = models.pop(i)
            return removed
    raise ValueError(f"Model '{slug}' not found in catalog")


def reorder_models(catalog_data: Dict[str, Any], ordered_slugs: List[str]) -> None:
    models = catalog_data.get("models", [])
    slug_to_model = {m["slug"]: m for m in models if "slug" in m}
    new_models = []

    priority_counter = 1000
    for s in ordered_slugs:
        if s in slug_to_model:
            m = slug_to_model.pop(s)
            m["priority"] = priority_counter
            priority_counter += 1
            new_models.append(m)

    for remaining in slug_to_model.values():
        remaining["priority"] = priority_counter
        priority_counter += 1
        new_models.append(remaining)

    catalog_data["models"] = new_models


def move_model(
    catalog_data: Dict[str, Any],
    slug: str,
    direction: Optional[str] = None,
    target_index: Optional[int] = None
) -> int:
    models = catalog_data.get("models", [])
    current_idx = -1
    for i, m in enumerate(models):
        if m.get("slug") == slug:
            current_idx = i
            break

    if current_idx < 0:
        raise ValueError(f"Model '{slug}' not found in catalog")

    if target_index is not None:
        new_idx = max(0, min(target_index, len(models) - 1))
    elif direction == "up":
        new_idx = max(0, current_idx - 1)
    elif direction == "down":
        new_idx = min(len(models) - 1, current_idx + 1)
    elif direction == "top":
        new_idx = 0
    elif direction == "bottom":
        new_idx = len(models) - 1
    else:
        raise ValueError("Must specify direction ('up', 'down', 'top', 'bottom') or target_index")

    if new_idx != current_idx:
        item = models.pop(current_idx)
        models.insert(new_idx, item)

    # 规范化所有 priority 从 1000 开始单调递增
    for idx, m in enumerate(models):
        m["priority"] = 1000 + idx

    return new_idx
