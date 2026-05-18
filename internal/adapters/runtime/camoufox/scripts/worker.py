import json
import os
import re
import sys
import traceback
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from playwright.sync_api import Error as PlaywrightError
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
from playwright.sync_api import sync_playwright


ERROR_VALIDATION_FAILED = "validation_failed"
ERROR_BROWSER_UNAVAILABLE = "browser_unavailable"
ERROR_NAVIGATION_FAILED = "navigation_failed"
ERROR_SCRIPT_FAILED = "script_failed"
ERROR_TIMEOUT = "timeout"
ERROR_UNSUPPORTED_OPERATION = "unsupported_operation"


class CommandFailure(Exception):
    def __init__(self, code: str, message: str, retryable: bool = False):
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable


def main() -> None:
    raw_options = os.environ.get("CAMOUFOX_WORKER_OPTIONS_JSON", "{}")
    options = json.loads(raw_options)
    endpoint = options["endpoint"]
    artifacts_dir = Path(options["artifacts_dir"])
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    context_options = options.get("context_options") or {}

    with sync_playwright() as playwright:
        browser = playwright.firefox.connect(endpoint)
        context = browser.new_context(**context_options)
        page = context.new_page()
        write_json({"type": "ready"})
        try:
            for line in sys.stdin:
                if not line.strip():
                    continue
                request = json.loads(line)
                if request.get("type") == "stop":
                    break
                if request.get("type") != "execute_task":
                    write_json(
                        {
                            "type": "task_result",
                            "error": {
                                "code": ERROR_VALIDATION_FAILED,
                                "message": "unsupported worker request type",
                                "retryable": False,
                            },
                        }
                    )
                    continue
                write_json(execute_task(page, artifacts_dir, request["task"]))
        finally:
            context.close()
            browser.close()


def execute_task(page: Any, artifacts_dir: Path, task: Dict[str, Any]) -> Dict[str, Any]:
    task_id = task.get("task_id") or ""
    input_data = task.get("input") or {}
    commands = input_data.get("commands") or []
    results: List[Dict[str, Any]] = []
    artifacts: List[Dict[str, Any]] = []

    for command in commands:
        try:
            result, artifact = execute_command(page, artifacts_dir, task_id, command)
            results.append(result)
            if artifact:
                artifacts.append(artifact)
        except CommandFailure as exc:
            results.append(failed_result(command, exc.code, exc.message, exc.retryable))
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": exc.code, "message": exc.message, "retryable": exc.retryable},
            }
        except PlaywrightTimeoutError as exc:
            results.append(failed_result(command, ERROR_TIMEOUT, str(exc), True))
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": ERROR_TIMEOUT, "message": str(exc), "retryable": True},
            }
        except PlaywrightError as exc:
            code = operation_error_code(command)
            results.append(failed_result(command, code, str(exc), is_retryable(code)))
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": code, "message": str(exc), "retryable": is_retryable(code)},
            }
        except Exception as exc:
            traceback.print_exc(file=sys.stderr)
            results.append(failed_result(command, ERROR_BROWSER_UNAVAILABLE, str(exc), True))
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": ERROR_BROWSER_UNAVAILABLE, "message": str(exc), "retryable": True},
            }

    return {"type": "task_result", "task_id": task_id, "results": results, "artifacts": artifacts}


def execute_command(page: Any, artifacts_dir: Path, task_id: str, command: Dict[str, Any]) -> Tuple[Dict[str, Any], Optional[Dict[str, Any]]]:
    if "navigate" in command:
        payload = command["navigate"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        wait_until = wait_until_value(payload.get("wait_until"))
        if wait_until:
            kwargs["wait_until"] = wait_until
        page.goto(required(payload, "url"), **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "click" in command:
        payload = command["click"]
        locator = resolve_locator(page, payload.get("selector"))
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        click_count = payload.get("click_count")
        if click_count:
            kwargs["click_count"] = click_count
        if payload.get("force"):
            kwargs["force"] = True
        locator.click(**kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "fill" in command:
        payload = command["fill"]
        locator = resolve_locator(page, payload.get("selector"))
        locator.fill(payload.get("value") or "", **timeout_kwargs(payload.get("timeout") or command.get("timeout")))
        return succeeded_result(command, current_url=page.url), None

    if "press" in command:
        payload = command["press"]
        key = required(payload, "key")
        selector = payload.get("selector")
        if selector and selector.get("value"):
            resolve_locator(page, selector).press(key, **timeout_kwargs(payload.get("timeout") or command.get("timeout")))
        else:
            page.keyboard.press(key)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_selector" in command:
        payload = command["wait_for_selector"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        state = selector_state_value(payload.get("state"))
        if state:
            kwargs["state"] = state
        resolve_locator(page, payload.get("selector")).wait_for(**kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_text" in command:
        payload = command["wait_for_text"]
        page.get_by_text(required(payload, "text"), exact=bool(payload.get("exact"))).wait_for(
            **timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        )
        return succeeded_result(command, current_url=page.url), None

    if "extract_text" in command:
        payload = command["extract_text"]
        locator = resolve_locator(page, payload.get("selector"))
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        count = locator.count()
        if payload.get("all_matches"):
            texts = locator.all_text_contents()
            return succeeded_result(command, texts=texts, matched_count=count, current_url=page.url), None
        text = locator.first.inner_text(timeout=timeout)
        return succeeded_result(command, text=text, matched_count=count, current_url=page.url), None

    if "screenshot" in command:
        payload = command["screenshot"]
        artifact_key = payload.get("artifact_key") or command.get("command_id") or "screenshot"
        artifact_id = sanitize_filename(f"{task_id}-{artifact_key}")
        path = artifacts_dir / f"{artifact_id}.png"
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        selector = payload.get("selector")
        if selector and selector.get("value"):
            resolve_locator(page, selector).screenshot(path=str(path), **kwargs)
        else:
            page.screenshot(path=str(path), full_page=bool(payload.get("full_page")), **kwargs)
        artifact = {
            "artifact_id": artifact_id,
            "kind": "BROWSER_ARTIFACT_KIND_SCREENSHOT",
            "uri": path.resolve().as_uri(),
            "content_type": "image/png",
            "size_bytes": path.stat().st_size,
            "labels": {"task_id": task_id, "command_id": command.get("command_id") or ""},
            "created_at": now_rfc3339(),
        }
        return succeeded_result(command, artifact=artifact, current_url=page.url), artifact

    if "upload_file" in command:
        raise CommandFailure(ERROR_UNSUPPORTED_OPERATION, "upload_file requires a secret/file resolver adapter", False)

    if "evaluate" in command:
        payload = command["evaluate"]
        expression = required(payload, "expression")
        arg = payload.get("args")
        value = page.evaluate(expression, arg)
        return succeeded_result(command, json_value=json_safe(value), current_url=page.url), None

    raise CommandFailure(ERROR_UNSUPPORTED_OPERATION, "unsupported command operation", False)


def resolve_locator(page: Any, selector: Optional[Dict[str, Any]]) -> Any:
    if not selector:
        raise CommandFailure(ERROR_VALIDATION_FAILED, "selector is required", False)
    value = required(selector, "value")
    kind = selector.get("kind") or "BROWSER_SELECTOR_KIND_CSS"
    exact = bool(selector.get("exact"))
    timeout = duration_ms(selector.get("timeout"))
    if kind == "BROWSER_SELECTOR_KIND_TEXT":
        locator = page.get_by_text(value, exact=exact)
    elif kind == "BROWSER_SELECTOR_KIND_ROLE":
        role_name = selector.get("role_name") or value
        name = value if selector.get("role_name") else None
        locator = page.get_by_role(role_name, name=name, exact=exact)
    elif kind == "BROWSER_SELECTOR_KIND_LABEL":
        locator = page.get_by_label(value, exact=exact)
    elif kind == "BROWSER_SELECTOR_KIND_PLACEHOLDER":
        locator = page.get_by_placeholder(value, exact=exact)
    elif kind == "BROWSER_SELECTOR_KIND_TEST_ID":
        locator = page.get_by_test_id(value)
    elif kind == "BROWSER_SELECTOR_KIND_XPATH":
        locator = page.locator(value if value.startswith("xpath=") else f"xpath={value}")
    else:
        locator = page.locator(value)
    if timeout is not None:
        locator.first.wait_for(timeout=timeout)
    return locator


def timeout_kwargs(value: Any) -> Dict[str, Any]:
    timeout = duration_ms(value)
    if timeout is None:
        return {}
    return {"timeout": timeout}


def duration_ms(value: Any) -> Optional[float]:
    if value is None or value == "":
        return None
    if isinstance(value, (int, float)):
        return float(value) * 1000
    if isinstance(value, dict):
        seconds = float(value.get("seconds") or 0)
        nanos = float(value.get("nanos") or 0)
        return seconds * 1000 + nanos / 1_000_000
    if isinstance(value, str):
        match = re.fullmatch(r"([-+]?\d+(?:\.\d+)?)s", value)
        if match:
            return float(match.group(1)) * 1000
    raise CommandFailure(ERROR_VALIDATION_FAILED, f"invalid duration: {value}", False)


def wait_until_value(value: Optional[str]) -> Optional[str]:
    mapping = {
        "BROWSER_NAVIGATION_WAIT_UNTIL_LOAD": "load",
        "BROWSER_NAVIGATION_WAIT_UNTIL_DOM_CONTENT_LOADED": "domcontentloaded",
        "BROWSER_NAVIGATION_WAIT_UNTIL_NETWORK_IDLE": "networkidle",
        "BROWSER_NAVIGATION_WAIT_UNTIL_COMMIT": "commit",
    }
    return mapping.get(value or "")


def selector_state_value(value: Optional[str]) -> Optional[str]:
    mapping = {
        "BROWSER_SELECTOR_STATE_ATTACHED": "attached",
        "BROWSER_SELECTOR_STATE_DETACHED": "detached",
        "BROWSER_SELECTOR_STATE_VISIBLE": "visible",
        "BROWSER_SELECTOR_STATE_HIDDEN": "hidden",
    }
    return mapping.get(value or "")


def succeeded_result(command: Dict[str, Any], **kwargs: Any) -> Dict[str, Any]:
    result = {
        "command_id": command.get("command_id") or "",
        "command_key": command.get("command_key") or "",
        "status": "BROWSER_COMMAND_STATUS_SUCCEEDED",
        "completed_at": now_rfc3339(),
    }
    result.update({key: value for key, value in kwargs.items() if value is not None})
    return result


def failed_result(command: Dict[str, Any], code: str, message: str, retryable: bool) -> Dict[str, Any]:
    return {
        "command_id": command.get("command_id") or "",
        "command_key": command.get("command_key") or "",
        "status": "BROWSER_COMMAND_STATUS_FAILED",
        "error": {"code": proto_error_code(code), "message": message, "retryable": retryable},
        "completed_at": now_rfc3339(),
    }


def proto_error_code(code: str) -> str:
    mapping = {
        ERROR_VALIDATION_FAILED: "BROWSER_AUTOMATION_ERROR_CODE_VALIDATION_FAILED",
        ERROR_BROWSER_UNAVAILABLE: "BROWSER_AUTOMATION_ERROR_CODE_BROWSER_UNAVAILABLE",
        ERROR_NAVIGATION_FAILED: "BROWSER_AUTOMATION_ERROR_CODE_NAVIGATION_FAILED",
        ERROR_SCRIPT_FAILED: "BROWSER_AUTOMATION_ERROR_CODE_SCRIPT_FAILED",
        ERROR_TIMEOUT: "BROWSER_AUTOMATION_ERROR_CODE_TIMEOUT",
        ERROR_UNSUPPORTED_OPERATION: "BROWSER_AUTOMATION_ERROR_CODE_UNSUPPORTED_OPERATION",
    }
    return mapping.get(code, "BROWSER_AUTOMATION_ERROR_CODE_INTERNAL")


def operation_error_code(command: Dict[str, Any]) -> str:
    if "navigate" in command:
        return ERROR_NAVIGATION_FAILED
    if "evaluate" in command:
        return ERROR_SCRIPT_FAILED
    return ERROR_BROWSER_UNAVAILABLE


def is_retryable(code: str) -> bool:
    return code in {ERROR_BROWSER_UNAVAILABLE, ERROR_NAVIGATION_FAILED, ERROR_TIMEOUT}


def required(payload: Dict[str, Any], key: str) -> str:
    value = payload.get(key)
    if not value:
        raise CommandFailure(ERROR_VALIDATION_FAILED, f"{key} is required", False)
    return value


def json_safe(value: Any) -> Any:
    try:
        json.dumps(value)
        return value
    except TypeError:
        return str(value)


def sanitize_filename(value: str) -> str:
    value = re.sub(r"[^A-Za-z0-9_.-]+", "-", value).strip("-")
    return value or "artifact"


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def write_json(value: Dict[str, Any]) -> None:
    print(json.dumps(value, separators=(",", ":")), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        traceback.print_exc(file=sys.stderr)
        write_json(
            {
                "type": "task_result",
                "error": {"code": ERROR_BROWSER_UNAVAILABLE, "message": str(exc), "retryable": True},
            }
        )
