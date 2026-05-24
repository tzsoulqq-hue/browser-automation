import json
import os
import re
import sys
import time
import traceback
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple

from playwright.sync_api import Error as PlaywrightError
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
from playwright.sync_api import sync_playwright


ERROR_VALIDATION_FAILED = "validation_failed"
ERROR_BROWSER_UNAVAILABLE = "browser_unavailable"
ERROR_NAVIGATION_FAILED = "navigation_failed"
ERROR_SCRIPT_FAILED = "script_failed"
ERROR_TIMEOUT = "timeout"
ERROR_UNSUPPORTED_OPERATION = "unsupported_operation"
ACTIONABLE_CLICK_SELECTOR = (
    "a,button,input[type=button],input[type=submit],input[type=reset],"
    "[role=button],[role=link],[role=menuitem]"
)


class CommandFailure(Exception):
    def __init__(self, code: str, message: str, retryable: bool = False):
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable


class NetworkLog:
    def __init__(self, page: Any):
        self.page = page
        self.events: List[Dict[str, Any]] = []
        page.on("request", self.on_request)
        page.on("response", self.on_response)
        page.on("requestfinished", self.on_request_finished)
        page.on("requestfailed", self.on_request_failed)

    def on_request(self, request: Any) -> None:
        event = {
            "request_id": str(id(request)),
            "url": sanitize_url(str(getattr(request, "url", ""))),
            "_url": str(getattr(request, "url", "")),
            "method": str(getattr(request, "method", "")).upper(),
            "resource_type": str(getattr(request, "resource_type", "")),
            "phase": "started",
            "started_at_unix_ms": now_unix_ms(),
        }
        self.events.append(event)
        del self.events[:-300]

    def on_response(self, response: Any) -> None:
        request = getattr(response, "request", None)
        event = self.event_for_request(request)
        if not event:
            return
        event["status_code"] = int(getattr(response, "status", 0) or 0)
        event["response_at_unix_ms"] = now_unix_ms()

    def on_request_finished(self, request: Any) -> None:
        event = self.event_for_request(request)
        if not event:
            return
        event["phase"] = "finished"
        event["finished_at_unix_ms"] = now_unix_ms()
        try:
            response = request.response()
            if response:
                event["status_code"] = int(getattr(response, "status", 0) or 0)
        except (PlaywrightError, PlaywrightTimeoutError):
            pass

    def on_request_failed(self, request: Any) -> None:
        event = self.event_for_request(request)
        if not event:
            return
        event["phase"] = "failed"
        event["failed_at_unix_ms"] = now_unix_ms()
        failure = getattr(request, "failure", None)
        if failure:
            event["failure"] = str(failure)

    def event_for_request(self, request: Any) -> Optional[Dict[str, Any]]:
        if request is None:
            return None
        request_id = str(id(request))
        for event in reversed(self.events):
            if event.get("request_id") == request_id:
                return event
        return None

    def wait_for(self, request_filter: Dict[str, Any], require_response: bool, timeout_value: Any) -> Dict[str, Any]:
        timeout = duration_ms(timeout_value)
        if timeout is None:
            timeout = 5000
        deadline = time.monotonic() + timeout / 1000
        while True:
            event = self.first_matching(request_filter, require_response)
            if event:
                return public_network_event(event)
            if time.monotonic() >= deadline:
                raise CommandFailure(ERROR_TIMEOUT, "network request did not match", True)
            self.page.wait_for_timeout(100)

    def list(self, request_filter: Dict[str, Any], limit: int) -> List[Dict[str, Any]]:
        matches = [public_network_event(event) for event in self.events if network_event_matches(event, request_filter, False)]
        if limit > 0:
            return matches[-limit:]
        return matches

    def first_matching(self, request_filter: Dict[str, Any], require_response: bool) -> Optional[Dict[str, Any]]:
        for event in self.events:
            if network_event_matches(event, request_filter, require_response):
                return event
        return None


def main() -> None:
    raw_options = os.environ.get("CAMOUFOX_WORKER_OPTIONS_JSON", "{}")
    options = json.loads(raw_options)
    endpoint = options["endpoint"]
    artifacts_dir = Path(options["artifacts_dir"])
    artifacts_dir.mkdir(parents=True, exist_ok=True)
    context_options = options.get("context_options") or {}
    init_scripts = options.get("init_scripts") or []

    with sync_playwright() as playwright:
        browser = playwright.firefox.connect(endpoint)
        context = browser.new_context(**context_options)
        for script in init_scripts:
            if script:
                context.add_init_script(script)
        page = context.new_page()
        network_log = NetworkLog(page)
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
                write_json(execute_task(page, network_log, artifacts_dir, request["task"]))
        finally:
            context.close()
            browser.close()


def execute_task(page: Any, network_log: "NetworkLog", artifacts_dir: Path, task: Dict[str, Any]) -> Dict[str, Any]:
    task_id = task.get("task_id") or ""
    input_data = task.get("input") or {}
    commands = input_data.get("commands") or []
    results: List[Dict[str, Any]] = []
    artifacts: List[Dict[str, Any]] = []

    for command in commands:
        try:
            result, artifact = execute_command(page, network_log, artifacts_dir, task_id, command)
            results.append(result)
            if artifact:
                artifacts.append(artifact)
        except CommandFailure as exc:
            results.append(failed_result(command, exc.code, exc.message, exc.retryable))
            if command.get("continue_on_error"):
                continue
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": exc.code, "message": exc.message, "retryable": exc.retryable},
            }
        except PlaywrightTimeoutError as exc:
            results.append(failed_result(command, ERROR_TIMEOUT, str(exc), True))
            if command.get("continue_on_error"):
                continue
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
            if command.get("continue_on_error"):
                continue
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
            if command.get("continue_on_error"):
                continue
            return {
                "type": "task_result",
                "task_id": task_id,
                "results": results,
                "artifacts": artifacts,
                "error": {"code": ERROR_BROWSER_UNAVAILABLE, "message": str(exc), "retryable": True},
            }

    return {"type": "task_result", "task_id": task_id, "results": results, "artifacts": artifacts}


def execute_command(page: Any, network_log: "NetworkLog", artifacts_dir: Path, task_id: str, command: Dict[str, Any]) -> Tuple[Dict[str, Any], Optional[Dict[str, Any]]]:
    if "navigate" in command:
        payload = command["navigate"]
        kwargs = navigation_kwargs(payload, command)
        page.goto(required(payload, "url"), **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "reload" in command:
        payload = command["reload"]
        page.reload(**navigation_kwargs(payload, command))
        return succeeded_result(command, current_url=page.url), None

    if "go_back" in command:
        payload = command["go_back"]
        page.go_back(**navigation_kwargs(payload, command))
        return succeeded_result(command, current_url=page.url), None

    if "go_forward" in command:
        payload = command["go_forward"]
        page.go_forward(**navigation_kwargs(payload, command))
        return succeeded_result(command, current_url=page.url), None

    if "click" in command:
        payload = command["click"]
        locator = resolve_click_locator(page, payload)
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        click_count = payload.get("click_count")
        if click_count:
            kwargs["click_count"] = click_count
        if payload.get("force"):
            kwargs["force"] = True
        button = mouse_button_value(payload.get("button"))
        if button:
            kwargs["button"] = button
        position = point_value(payload.get("position"))
        if position:
            kwargs["position"] = position
        delay = duration_ms(payload.get("delay"))
        if delay is not None:
            kwargs["delay"] = delay
        hold_duration = duration_ms(payload.get("hold_duration"))
        if hold_duration is not None and hold_duration > 0:
            x, y = locator_point(locator, position, payload.get("timeout") or command.get("timeout"))
            page.mouse.move(x, y)
            page.mouse.down(button=button or "left")
            page.wait_for_timeout(hold_duration)
            page.mouse.up(button=button or "left")
            return succeeded_result(command, current_url=page.url), None
        click_locator(locator, **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "fill" in command:
        payload = command["fill"]
        locator = resolve_command_locator(page, payload)
        fill_locator(page, locator, payload.get("value") or "", payload.get("timeout") or command.get("timeout"))
        return succeeded_result(command, current_url=page.url), None

    if "set_checked" in command:
        payload = command["set_checked"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        if payload.get("force"):
            kwargs["force"] = True
        resolve_command_locator(page, payload).set_checked(bool(payload.get("checked")), **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "type_text" in command:
        payload = command["type_text"]
        text = required(payload, "text")
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        delay = duration_ms(payload.get("delay"))
        if delay is not None:
            kwargs["delay"] = delay
        if has_command_locator(payload):
            locator = resolve_command_locator(page, payload)
            if payload.get("clear_before"):
                locator.fill("", **timeout_kwargs(payload.get("timeout") or command.get("timeout")))
            locator.press_sequentially(text, **kwargs)
        else:
            keyboard_kwargs: Dict[str, Any] = {}
            if delay is not None:
                keyboard_kwargs["delay"] = delay
            page.keyboard.type(text, **keyboard_kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "clear" in command:
        payload = command["clear"]
        resolve_command_locator(page, payload).fill("", **timeout_kwargs(payload.get("timeout") or command.get("timeout")))
        return succeeded_result(command, current_url=page.url), None

    if "press" in command:
        payload = command["press"]
        key = required(payload, "key")
        if has_command_locator(payload):
            resolve_command_locator(page, payload).press(key, **timeout_kwargs(payload.get("timeout") or command.get("timeout")))
        else:
            page.keyboard.press(key)
        return succeeded_result(command, current_url=page.url), None

    if "focus" in command:
        payload = command["focus"]
        resolve_command_locator(page, payload).focus(**timeout_kwargs(payload.get("timeout") or command.get("timeout")))
        return succeeded_result(command, current_url=page.url), None

    if "blur" in command:
        payload = command["blur"]
        resolve_command_locator(page, payload).evaluate("(el) => el.blur()")
        return succeeded_result(command, current_url=page.url), None

    if "hover" in command:
        payload = command["hover"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        position = point_value(payload.get("position"))
        if position:
            kwargs["position"] = position
        resolve_command_locator(page, payload).hover(**kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "mouse_move" in command:
        payload = command["mouse_move"]
        points = [point_value(point) for point in payload.get("path") or []]
        if not points:
            points = [point_value(payload.get("point"))]
        points = [point for point in points if point]
        if not points:
            raise CommandFailure(ERROR_VALIDATION_FAILED, "mouse move point or path is required", False)
        wait_ms = duration_ms(payload.get("duration"))
        interval_ms = wait_ms / len(points) if wait_ms else None
        for point in points:
            page.mouse.move(point["x"], point["y"])
            if interval_ms:
                page.wait_for_timeout(interval_ms)
        return succeeded_result(command, current_url=page.url), None

    if "mouse_click" in command:
        payload = command["mouse_click"]
        point = required_point(payload.get("point"), "point")
        button = mouse_button_value(payload.get("button")) or "left"
        hold_duration = duration_ms(payload.get("hold_duration"))
        if hold_duration is not None and hold_duration > 0:
            page.mouse.move(point["x"], point["y"])
            page.mouse.down(button=button)
            page.wait_for_timeout(hold_duration)
            page.mouse.up(button=button)
        else:
            kwargs: Dict[str, Any] = {"button": button}
            click_count = payload.get("click_count")
            if click_count:
                kwargs["click_count"] = click_count
            delay = duration_ms(payload.get("delay"))
            if delay is not None:
                kwargs["delay"] = delay
            page.mouse.click(point["x"], point["y"], **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "mouse_down" in command:
        payload = command["mouse_down"]
        page.mouse.down(button=mouse_button_value(payload.get("button")) or "left")
        return succeeded_result(command, current_url=page.url), None

    if "mouse_up" in command:
        payload = command["mouse_up"]
        page.mouse.up(button=mouse_button_value(payload.get("button")) or "left")
        return succeeded_result(command, current_url=page.url), None

    if "drag" in command:
        payload = command["drag"]
        drag(page, payload, command)
        return succeeded_result(command, current_url=page.url), None

    if "scroll" in command:
        payload = command["scroll"]
        delta_x = float(payload.get("delta_x") or 0)
        delta_y = float(payload.get("delta_y") or 0)
        if has_command_locator(payload):
            locator = resolve_command_locator(page, payload)
            kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
            if delta_x == 0 and delta_y == 0:
                locator.scroll_into_view_if_needed(**kwargs)
            else:
                locator.scroll_into_view_if_needed(**kwargs)
                locator.hover(**kwargs)
                page.mouse.wheel(delta_x, delta_y)
        else:
            page.mouse.wheel(delta_x, delta_y)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_selector" in command:
        payload = command["wait_for_selector"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        state = selector_state_value(payload.get("state"))
        if state:
            kwargs["state"] = state
        resolve_command_locator(page, payload).wait_for(**kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_text" in command:
        payload = command["wait_for_text"]
        page.get_by_text(required(payload, "text"), exact=bool(payload.get("exact"))).wait_for(
            **timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        )
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_url" in command:
        payload = command["wait_for_url"]
        pattern = required(payload, "url_pattern")
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        if payload.get("exact"):
            page.wait_for_url(lambda url: str(url) == pattern, **kwargs)
        else:
            page.wait_for_url(pattern, **kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_load_state" in command:
        payload = command["wait_for_load_state"]
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        state = load_state_value(payload.get("state"))
        if state:
            page.wait_for_load_state(state, **kwargs)
        else:
            page.wait_for_load_state(**kwargs)
        return succeeded_result(command, current_url=page.url), None

    if "wait_for_timeout" in command:
        payload = command["wait_for_timeout"]
        duration = duration_ms(payload.get("duration"))
        if duration is None or duration <= 0:
            raise CommandFailure(ERROR_VALIDATION_FAILED, "wait timeout duration is required", False)
        page.wait_for_timeout(duration)
        return succeeded_result(command, current_url=page.url), None

    if "get_page_state" in command:
        payload = command["get_page_state"]
        state, title, text = collect_page_state(page, payload)
        return succeeded_result(command, current_url=page.url, title=title, text=text, json_value=state), None

    if "get_cookies" in command:
        payload = command["get_cookies"]
        urls = payload.get("urls") or []
        if urls:
            cookies = page.context.cookies(urls)
        else:
            cookies = page.context.cookies()
        return succeeded_result(command, json_value={"cookies": json_safe(cookies)}, current_url=page.url), None

    if "get_storage_state" in command:
        payload = command["get_storage_state"]
        state = page.context.storage_state()
        if not payload.get("include_cookies", True):
            state["cookies"] = []
        if not payload.get("include_origins", True):
            state["origins"] = []
        return succeeded_result(command, json_value=json_safe(state), current_url=page.url), None

    if "wait_for_network_request" in command:
        payload = command["wait_for_network_request"]
        timeout = payload.get("timeout") or command.get("timeout")
        request = network_log.wait_for(payload.get("filter") or {}, payload.get("require_response"), timeout)
        return succeeded_result(command, json_value={"request": request}, current_url=page.url), None

    if "get_network_requests" in command:
        payload = command["get_network_requests"]
        requests = network_log.list(payload.get("filter") or {}, int(payload.get("limit") or 0))
        return succeeded_result(command, json_value={"requests": requests}, matched_count=len(requests), current_url=page.url), None

    if "extract_text" in command:
        payload = command["extract_text"]
        locator = resolve_command_locator(page, payload)
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        count = locator.count()
        if payload.get("all_matches"):
            texts = locator.all_text_contents()
            return succeeded_result(command, texts=texts, matched_count=count, current_url=page.url), None
        text = locator.first.inner_text(timeout=timeout)
        return succeeded_result(command, text=text, matched_count=count, current_url=page.url), None

    if "count_elements" in command:
        payload = command["count_elements"]
        locator = resolve_command_locator(page, payload)
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        return succeeded_result(command, matched_count=locator.count(), current_url=page.url), None

    if "get_attribute" in command:
        payload = command["get_attribute"]
        name = required(payload, "name")
        locator = resolve_command_locator(page, payload)
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        count = locator.count()
        if payload.get("all_matches"):
            values = [locator.nth(index).get_attribute(name, timeout=timeout) for index in range(count)]
            return succeeded_result(command, json_value={"values": values}, matched_count=count, current_url=page.url), None
        value = locator.first.get_attribute(name, timeout=timeout)
        return succeeded_result(command, attribute_value=value, matched_count=count, current_url=page.url), None

    if "extract_element" in command:
        payload = command["extract_element"]
        locator = resolve_command_locator(page, payload)
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        count = locator.count()
        if payload.get("all_matches"):
            elements = [extract_element(locator.nth(index), payload, timeout) for index in range(count)]
            return succeeded_result(command, json_value={"elements": elements}, matched_count=count, current_url=page.url), None
        element = extract_element(locator.first, payload, timeout)
        return succeeded_result(
            command,
            text=element.get("text"),
            json_value=element,
            matched_count=count,
            attributes=element.get("attributes"),
            visible=element.get("visible"),
            bounding_box=element.get("bounding_box"),
            current_url=page.url,
        ), None

    if "screenshot" in command:
        payload = command["screenshot"]
        artifact_key = payload.get("artifact_key") or command.get("command_id") or "screenshot"
        artifact_id = sanitize_filename(f"{task_id}-{artifact_key}")
        path = artifacts_dir / f"{artifact_id}.png"
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        if has_command_locator(payload):
            resolve_command_locator(page, payload).screenshot(path=str(path), **kwargs)
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

    if "select_option" in command:
        payload = command["select_option"]
        locator = resolve_command_locator(page, payload)
        kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
        selected_values = select_options(locator, payload.get("values") or [], payload.get("labels") or [], payload.get("indexes") or [], kwargs)
        return succeeded_result(command, json_value={"selected_values": selected_values}, current_url=page.url), None

    if "submit_form" in command:
        payload = command["submit_form"]
        locator = resolve_command_locator(page, payload)
        timeout = duration_ms(payload.get("timeout") or command.get("timeout"))
        if timeout is not None:
            locator.first.wait_for(timeout=timeout)
        locator.first.evaluate(
            """(el) => {
                const form = el.tagName && el.tagName.toLowerCase() === 'form' ? el : el.closest('form');
                if (!form) throw new Error('form not found');
                if (typeof form.requestSubmit === 'function') form.requestSubmit();
                else form.submit();
            }"""
        )
        return succeeded_result(command, current_url=page.url), None

    if "evaluate" in command:
        payload = command["evaluate"]
        expression = required(payload, "expression")
        arg = payload.get("args")
        value = evaluate_page(page, expression, arg)
        return succeeded_result(command, json_value=json_safe(value), current_url=page.url), None

    raise CommandFailure(ERROR_UNSUPPORTED_OPERATION, "unsupported command operation", False)


def has_command_locator(payload: Dict[str, Any]) -> bool:
    selector = payload.get("selector") or {}
    selector_group = payload.get("selector_group") or {}
    if selector.get("value"):
        return True
    return any((candidate or {}).get("value") for candidate in selector_group.get("selectors") or [])


def click_locator(locator: Any, **kwargs: Any) -> None:
    locator.first.click(**kwargs)


def fill_locator(page: Any, locator: Any, value: str, timeout_value: Any) -> None:
    kwargs = timeout_kwargs(timeout_value)
    try:
        locator.fill(value, **kwargs)
        return
    except (PlaywrightError, PlaywrightTimeoutError):
        pass

    try:
        locator.first.evaluate(
            """(el, value) => {
                el.focus();
                const proto = el instanceof HTMLTextAreaElement
                    ? HTMLTextAreaElement.prototype
                    : HTMLInputElement.prototype;
                const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
                if (setter) {
                    setter.call(el, value);
                } else {
                    el.value = value;
                }
                el.dispatchEvent(new Event("input", {bubbles: true}));
                el.dispatchEvent(new Event("change", {bubbles: true}));
            }""",
            value,
        )
        return
    except (PlaywrightError, PlaywrightTimeoutError):
        pass

    locator.first.focus(**kwargs)
    page.keyboard.press("Control+A")
    page.keyboard.press("Delete")
    page.keyboard.type(value)


def select_options(locator: Any, values: List[str], labels: List[str], indexes: List[int], kwargs: Dict[str, Any]) -> List[str]:
    attempts: List[Tuple[str, Any]] = []
    if values:
        attempts.append(("value", values))
        attempts.extend(("value", value) for value in values)
    if labels:
        attempts.append(("label", labels))
        attempts.extend(("label", label) for label in labels)
    if indexes:
        attempts.append(("index", indexes))
        attempts.extend(("index", index) for index in indexes)
    last_error: Optional[Exception] = None
    for kind, value in attempts:
        try:
            if kind == "value":
                return list(locator.select_option(value=value, **kwargs))
            if kind == "label":
                return list(locator.select_option(label=value, **kwargs))
            return list(locator.select_option(index=value, **kwargs))
        except (PlaywrightError, PlaywrightTimeoutError) as exc:
            last_error = exc
    if last_error:
        raise last_error
    return []


def with_navigation_retry(page: Any, action: Callable[[], Any]) -> Any:
    deadline = time.monotonic() + 15
    last_error: Optional[Exception] = None
    for attempt in range(3):
        try:
            return action()
        except (PlaywrightError, PlaywrightTimeoutError) as exc:
            if not is_navigation_context_error(exc) or attempt == 2:
                raise
            last_error = exc
            wait_for_navigation_settle(page, deadline)
    if last_error:
        raise last_error
    raise CommandFailure(ERROR_BROWSER_UNAVAILABLE, "browser action did not return a result", True)


def wait_for_navigation_settle(page: Any, deadline: float) -> None:
    wait_ms = max(100, min(5000, int((deadline - time.monotonic()) * 1000)))
    try:
        page.wait_for_load_state("domcontentloaded", timeout=wait_ms)
    except (PlaywrightError, PlaywrightTimeoutError):
        pass
    try:
        page.wait_for_timeout(500)
    except (PlaywrightError, PlaywrightTimeoutError):
        pass


def evaluate_page(page: Any, expression: str, arg: Any) -> Any:
    return with_navigation_retry(page, lambda: page.evaluate(expression, arg))


def is_navigation_context_error(exc: Exception) -> bool:
    message = str(exc).lower()
    return "execution context was destroyed" in message or "most likely because of a navigation" in message


def resolve_command_locator(page: Any, payload: Dict[str, Any]) -> Any:
    selector_group = payload.get("selector_group")
    if selector_group and selector_group.get("selectors"):
        return resolve_locator_group(page, selector_group)
    return resolve_locator(page, payload.get("selector"))


def resolve_click_locator(page: Any, payload: Dict[str, Any]) -> Any:
    selector_group = payload.get("selector_group")
    if selector_group and selector_group.get("selectors"):
        return resolve_click_locator_group(page, selector_group)
    return resolve_click_locator_from_selector(page, payload.get("selector"))


def resolve_click_locator_group(page: Any, selector_group: Dict[str, Any]) -> Any:
    selectors = [selector for selector in selector_group.get("selectors") or [] if selector and selector.get("value")]
    if not selectors:
        raise CommandFailure(ERROR_VALIDATION_FAILED, "selector_group.selectors is required", False)
    if selector_group.get("require_all"):
        return resolve_locator_group(page, selector_group)
    timeout = selector_group.get("timeout")
    failures: List[str] = []
    for selector in selectors:
        try:
            return resolve_click_locator_from_selector(page, with_default_timeout(selector, timeout))
        except (CommandFailure, PlaywrightError, PlaywrightTimeoutError) as exc:
            failures.append(str(exc))
    raise CommandFailure(ERROR_TIMEOUT, "; ".join(failures) or "selector group did not match", True)


def resolve_click_locator_from_selector(page: Any, selector: Optional[Dict[str, Any]]) -> Any:
    if selector and selector.get("kind") == "BROWSER_SELECTOR_KIND_TEXT":
        return action_text_locator(page, selector)
    return locator_for_selector(page, selector)


def resolve_action_text_locator(page: Any, selector: Dict[str, Any]) -> Any:
    locator = action_text_locator(page, selector)
    timeout = duration_ms(selector.get("timeout"))
    if timeout is not None:
        locator.first.wait_for(timeout=timeout)
    return locator


def action_text_locator(page: Any, selector: Dict[str, Any]) -> Any:
    value = required(selector, "value")
    exact = bool(selector.get("exact"))
    has_text: Any = re.compile(rf"^\s*{re.escape(value)}\s*$") if exact else value
    return page.locator(ACTIONABLE_CLICK_SELECTOR).filter(has_text=has_text)


def collect_page_state(page: Any, payload: Dict[str, Any]) -> Tuple[Dict[str, Any], Optional[str], Optional[str]]:
    def read_state() -> Tuple[Dict[str, Any], Optional[str], Optional[str]]:
        state: Dict[str, Any] = {"url": page.url}
        title: Optional[str] = None
        text: Optional[str] = None
        if payload.get("include_title"):
            title = page.title()
            state["title"] = title
        if payload.get("include_text"):
            text = page.locator("body").inner_text()
            state["text"] = text
        if payload.get("include_html"):
            state["html"] = page.content()
        state["inputs"] = collect_page_inputs(page)
        state["actions"] = collect_page_actions(page)
        return state, title, text

    return with_navigation_retry(page, read_state)


def collect_page_inputs(page: Any) -> List[Dict[str, str]]:
    return with_navigation_retry(page, lambda: page.evaluate(
        """() => {
            const visible = (el) => {
                const style = window.getComputedStyle(el);
                const rect = el.getBoundingClientRect();
                return style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0;
            };
            const compact = (value) => String(value || "").replace(/\\s+/g, " ").trim();
            return Array.from(document.querySelectorAll("input,textarea,select,[contenteditable='true'],[role='textbox'],[role='combobox']"))
                .filter(visible)
                .slice(0, 20)
                .map((el) => {
                    const item = {
                        tag: el.tagName.toLowerCase(),
                        type: compact(el.getAttribute("type")),
                        name: compact(el.getAttribute("name")),
                        id: compact(el.getAttribute("id")),
                        placeholder: compact(el.getAttribute("placeholder")),
                        ariaLabel: compact(el.getAttribute("aria-label")),
                        autocomplete: compact(el.getAttribute("autocomplete")),
                        role: compact(el.getAttribute("role")),
                    };
                    return Object.fromEntries(Object.entries(item).filter(([, value]) => value));
                })
                .filter((item) => Object.keys(item).length > 0);
        }"""
    ))


def collect_page_actions(page: Any) -> List[Dict[str, str]]:
    return with_navigation_retry(page, lambda: page.evaluate(
        """() => {
            const visible = (el) => {
                const style = window.getComputedStyle(el);
                const rect = el.getBoundingClientRect();
                return style.visibility !== "hidden" && style.display !== "none" && rect.width > 0 && rect.height > 0;
            };
            const compact = (value) => String(value || "").replace(/\\s+/g, " ").trim();
            const selector = "a,button,input[type=button],input[type=submit],input[type=reset],[role=button],[role=link],[role=menuitem]";
            return Array.from(document.querySelectorAll(selector))
                .filter(visible)
                .slice(0, 40)
                .map((el) => {
                    const item = {
                        tag: el.tagName.toLowerCase(),
                        text: compact(el.innerText || el.textContent),
                        ariaLabel: compact(el.getAttribute("aria-label")),
                        title: compact(el.getAttribute("title")),
                        type: compact(el.getAttribute("type")),
                        role: compact(el.getAttribute("role")),
                    };
                    return Object.fromEntries(Object.entries(item).filter(([, value]) => value));
                })
                .filter((item) => Object.keys(item).length > 0);
        }"""
    ))


def resolve_named_locator(page: Any, payload: Dict[str, Any], selector_key: str, selector_group_key: str) -> Any:
    selector_group = payload.get(selector_group_key)
    if selector_group and selector_group.get("selectors"):
        return resolve_locator_group(page, selector_group)
    return resolve_locator(page, payload.get(selector_key))


def resolve_locator_group(page: Any, selector_group: Dict[str, Any]) -> Any:
    selectors = [selector for selector in selector_group.get("selectors") or [] if selector and selector.get("value")]
    if not selectors:
        raise CommandFailure(ERROR_VALIDATION_FAILED, "selector_group.selectors is required", False)
    timeout = selector_group.get("timeout")
    if selector_group.get("require_all"):
        locators = [resolve_locator(page, with_default_timeout(selector, timeout)) for selector in selectors]
        return locators[0]
    failures: List[str] = []
    for selector in selectors:
        try:
            return resolve_locator(page, with_default_timeout(selector, timeout))
        except (CommandFailure, PlaywrightError, PlaywrightTimeoutError) as exc:
            failures.append(str(exc))
    raise CommandFailure(ERROR_TIMEOUT, "; ".join(failures) or "selector group did not match", True)


def with_default_timeout(selector: Dict[str, Any], timeout: Any) -> Dict[str, Any]:
    if timeout is None or selector.get("timeout") is not None:
        return selector
    candidate = dict(selector)
    candidate["timeout"] = timeout
    return candidate


def resolve_locator(page: Any, selector: Optional[Dict[str, Any]]) -> Any:
    locator = locator_for_selector(page, selector)
    timeout = duration_ms((selector or {}).get("timeout"))
    if timeout is not None:
        locator.first.wait_for(timeout=timeout)
    return locator


def locator_for_selector(page: Any, selector: Optional[Dict[str, Any]]) -> Any:
    if not selector:
        raise CommandFailure(ERROR_VALIDATION_FAILED, "selector is required", False)
    value = required(selector, "value")
    kind = selector.get("kind") or "BROWSER_SELECTOR_KIND_CSS"
    exact = bool(selector.get("exact"))
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
    return locator


def navigation_kwargs(payload: Dict[str, Any], command: Dict[str, Any]) -> Dict[str, Any]:
    kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
    wait_until = wait_until_value(payload.get("wait_until"))
    if wait_until:
        kwargs["wait_until"] = wait_until
    return kwargs


def point_value(value: Any) -> Optional[Dict[str, float]]:
    if not value:
        return None
    return {"x": float(value.get("x") or 0), "y": float(value.get("y") or 0)}


def required_point(value: Any, name: str) -> Dict[str, float]:
    point = point_value(value)
    if not point:
        raise CommandFailure(ERROR_VALIDATION_FAILED, f"{name} is required", False)
    return point


def locator_point(locator: Any, position: Optional[Dict[str, float]], timeout_value: Any) -> Tuple[float, float]:
    timeout = duration_ms(timeout_value)
    if timeout is not None:
        locator.first.wait_for(timeout=timeout)
    box = locator.first.bounding_box(timeout=timeout)
    if not box:
        raise CommandFailure(ERROR_TIMEOUT, "element bounding box is unavailable", True)
    if position:
        return float(box["x"]) + position["x"], float(box["y"]) + position["y"]
    return float(box["x"]) + float(box["width"]) / 2, float(box["y"]) + float(box["height"]) / 2


def drag(page: Any, payload: Dict[str, Any], command: Dict[str, Any]) -> None:
    kwargs = timeout_kwargs(payload.get("timeout") or command.get("timeout"))
    has_source_selector = bool((payload.get("source_selector") or {}).get("value")) or bool(
        (payload.get("source_selector_group") or {}).get("selectors")
    )
    has_target_selector = bool((payload.get("target_selector") or {}).get("value")) or bool(
        (payload.get("target_selector_group") or {}).get("selectors")
    )
    if has_source_selector and has_target_selector:
        source = resolve_named_locator(page, payload, "source_selector", "source_selector_group")
        target = resolve_named_locator(page, payload, "target_selector", "target_selector_group")
        source.drag_to(target, **kwargs)
        return
    source_point = point_value(payload.get("source_point"))
    target_point = point_value(payload.get("target_point"))
    if has_source_selector:
        source = resolve_named_locator(page, payload, "source_selector", "source_selector_group")
        source_point = dict(zip(("x", "y"), locator_point(source, None, payload.get("timeout") or command.get("timeout"))))
    if has_target_selector:
        target = resolve_named_locator(page, payload, "target_selector", "target_selector_group")
        target_point = dict(zip(("x", "y"), locator_point(target, None, payload.get("timeout") or command.get("timeout"))))
    if not source_point or not target_point:
        raise CommandFailure(ERROR_VALIDATION_FAILED, "drag source and target are required", False)
    page.mouse.move(source_point["x"], source_point["y"])
    page.mouse.down()
    page.mouse.move(target_point["x"], target_point["y"], steps=10)
    page.mouse.up()


def extract_element(locator: Any, payload: Dict[str, Any], timeout: Optional[float]) -> Dict[str, Any]:
    item: Dict[str, Any] = {}
    if payload.get("include_text"):
        item["text"] = locator.inner_text(timeout=timeout)
    if payload.get("include_html"):
        item["html"] = locator.inner_html(timeout=timeout)
    attributes: Dict[str, str] = {}
    if payload.get("include_attributes"):
        attributes.update(locator.evaluate("(el) => Object.fromEntries(Array.from(el.attributes).map((attr) => [attr.name, attr.value]))"))
    for name in payload.get("attribute_names") or []:
        value = locator.get_attribute(name, timeout=timeout)
        if value is not None:
            attributes[name] = value
    if attributes:
        item["attributes"] = attributes
    if payload.get("include_bounding_box"):
        item["bounding_box"] = locator.bounding_box(timeout=timeout)
    if payload.get("include_visibility"):
        item["visible"] = locator.is_visible(timeout=timeout)
    return json_safe(item)


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


def load_state_value(value: Optional[str]) -> Optional[str]:
    mapping = {
        "BROWSER_LOAD_STATE_LOAD": "load",
        "BROWSER_LOAD_STATE_DOM_CONTENT_LOADED": "domcontentloaded",
        "BROWSER_LOAD_STATE_NETWORK_IDLE": "networkidle",
    }
    return mapping.get(value or "")


def mouse_button_value(value: Optional[str]) -> Optional[str]:
    mapping = {
        "BROWSER_MOUSE_BUTTON_LEFT": "left",
        "BROWSER_MOUSE_BUTTON_RIGHT": "right",
        "BROWSER_MOUSE_BUTTON_MIDDLE": "middle",
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
    if any(operation in command for operation in ("navigate", "reload", "go_back", "go_forward", "wait_for_url", "wait_for_load_state")):
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


def network_event_matches(event: Dict[str, Any], request_filter: Dict[str, Any], require_response: bool) -> bool:
    if require_response and event.get("phase") not in {"finished", "failed"}:
        return False
    url = str(event.get("_url") or event.get("url") or "")
    url_substring = str(request_filter.get("url_substring") or "")
    if url_substring and url_substring not in url:
        return False
    url_regex = str(request_filter.get("url_regex") or "")
    if url_regex and not re.search(url_regex, url):
        return False
    method = str(request_filter.get("method") or "").upper()
    if method and str(event.get("method") or "").upper() != method:
        return False
    resource_type = str(request_filter.get("resource_type") or "")
    if resource_type and str(event.get("resource_type") or "") != resource_type:
        return False
    started_after = int(request_filter.get("started_after_unix_ms") or 0)
    if started_after > 0 and int(event.get("started_at_unix_ms") or 0) < started_after:
        return False
    status_min = int(request_filter.get("status_code_min") or 0)
    status_max = int(request_filter.get("status_code_max") or 0)
    if status_min > 0 or status_max > 0:
        status_code = int(event.get("status_code") or 0)
        if status_code <= 0:
            return False
        if status_min > 0 and status_code < status_min:
            return False
        if status_max > 0 and status_code > status_max:
            return False
    return True


def public_network_event(event: Dict[str, Any]) -> Dict[str, Any]:
    return {key: value for key, value in event.items() if not key.startswith("_")}


def sanitize_url(raw: str) -> str:
    raw = raw.strip()
    before, sep, _ = raw.partition("#")
    if sep:
        raw = before
    before, sep, _ = raw.partition("?")
    if sep:
        raw = before
    return raw


def sanitize_filename(value: str) -> str:
    value = re.sub(r"[^A-Za-z0-9_.-]+", "-", value).strip("-")
    return value or "artifact"


def now_unix_ms() -> int:
    return int(time.time() * 1000)


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
