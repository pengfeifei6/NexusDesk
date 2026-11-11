#!/usr/bin/env python3
"""Read-only API reference checks using Python's standard library.

Not a full OpenAPI/JSON Schema implementation or a running HTTP test. Run from
any directory; only repository source and the three API reference files are read.
"""
from __future__ import annotations

import json
import math
import re
import sys
from datetime import datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
METHODS = {"get", "post", "put", "patch", "delete", "head", "options"}
INT64_MAX = 9223372036854775807


class CheckFailure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise CheckFailure(message)


def canonical(path):
    """Parameter spelling differs in old index (:id) and actual handlers."""
    return re.sub(r":\w+|\{\w+\}", "{}", path)


def registered_routes():
    routes = []
    for source in sorted((ROOT / "internal").rglob("*.go")):
        if source.name.endswith("_test.go"):
            continue
        code = source.read_text()
        pattern = r"func\s+\([^)]*\)\s+RegisterRoutes\((\w+)\s+\*gin\.RouterGroup\)\s*\{"
        for match in re.finditer(pattern, code):
            # RegisterRoutes contains no nested string-literal braces today;
            # tokenization below still skips quoted strings and comments.
            start = match.end()
            tokens = re.finditer(r'"(?:\\.|[^"\\])*"|//[^\n]*|/\*[\s\S]*?\*/|[{}]', code[start:])
            depth, end = 1, None
            for token in tokens:
                if token.group() == "{":
                    depth += 1
                elif token.group() == "}":
                    depth -= 1
                    if depth == 0:
                        end = start + token.start()
                        break
            require(end is not None, f"unterminated RegisterRoutes: {source}")
            body = re.sub(r"//[^\n]*|/\*[\s\S]*?\*/", "", code[start:end])
            groups = {match[1]: "/api/v1"}
            call_pattern = (
                r'(?:(\w+)\s*:=\s*)?(\w+)\.'
                r'(Group|GET|POST|PATCH|DELETE|PUT|HEAD|OPTIONS)\("([^"\\]*)"'
            )
            for call in re.finditer(call_pattern, body):
                assigned, receiver, action, suffix = call.groups()
                require(receiver in groups, f"unresolved route group {receiver}: {source}")
                path = groups[receiver] + suffix
                if action == "Group":
                    require(assigned, f"unassigned route group: {source}")
                    groups[assigned] = path
                else:
                    routes.append((action.lower(), canonical(path)))
    require(len(routes) == len(set(routes)), "duplicate current Handler registration")
    return set(routes)


def indexed_routes():
    aliases = {
        "O": "/organizations/:orgID",
        "T": "/organizations/:orgID/tickets",
        "D": "/organizations/:orgID/knowledge/documents",
        "A": "/organizations/:orgID/ai",
    }
    result = []
    text = (ROOT / "docs/release/api-index.md").read_text()
    for method, path in re.findall(r"\|\s*\d+\s*\|\s*(GET|POST|PATCH|DELETE|PUT)\s*\|\s*([^|]+?)\s*\|", text):
        if path[0] in aliases:
            path = aliases[path[0]] + path[1:]
        result.append((method.lower(), canonical("/api/v1" + path)))
    require(len(result) == len(set(result)), "duplicate operation in canonical index")
    return set(result)


class Validator:
    """The subset used by these schemas, sufficient to validate their examples."""

    def __init__(self, document):
        self.document = document
        self.refs = 0
        self.examples = 0

    def resolve(self, reference):
        require(reference.startswith("#/"), f"external/nonlocal ref is unsupported: {reference}")
        value = self.document
        for part in reference[2:].split("/"):
            part = part.replace("~1", "/").replace("~0", "~")
            require(isinstance(value, dict) and part in value, f"unresolved ref: {reference}")
            value = value[part]
        return value

    def value(self, value, schema, label):
        if "$ref" in schema:
            self.value(value, self.resolve(schema["$ref"]), label)
        for keyword in ["anyOf", "oneOf"]:
            if keyword in schema:
                matched = 0
                for branch in schema[keyword]:
                    try:
                        self.value(value, branch, label)
                        matched += 1
                    except CheckFailure:
                        pass
                require(matched >= 1 if keyword == "anyOf" else matched == 1, f"{label}: {keyword} mismatch")
        for branch in schema.get("allOf", []):
            self.value(value, branch, label)
        kind = schema.get("type")
        types = {
            "object": isinstance(value, dict),
            "array": isinstance(value, list),
            "string": isinstance(value, str),
            "integer": isinstance(value, int) and not isinstance(value, bool),
            "number": isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value),
            "boolean": isinstance(value, bool),
            "null": value is None,
        }
        if kind:
            require(types.get(kind, False), f"{label}: expected {kind}, got {type(value).__name__}")
        if "const" in schema:
            require(value == schema["const"], f"{label}: const mismatch")
        if "enum" in schema:
            require(value in schema["enum"], f"{label}: enum mismatch")
        if kind in ("integer", "number"):
            require(value >= schema.get("minimum", -math.inf), f"{label}: below minimum")
            require(value <= schema.get("maximum", math.inf), f"{label}: above maximum")
        if kind == "string":
            require(len(value) >= schema.get("minLength", 0), f"{label}: string too short")
            require(len(value) <= schema.get("maxLength", math.inf), f"{label}: string too long")
            if "pattern" in schema:
                require(re.search(schema["pattern"], value) is not None, f"{label}: pattern mismatch")
            if schema.get("format") == "date-time":
                try:
                    datetime.fromisoformat(value.replace("Z", "+00:00"))
                except ValueError as error:
                    raise CheckFailure(f"{label}: invalid timestamp") from error
        if kind == "object":
            require(set(schema.get("required", [])) <= set(value), f"{label}: missing required fields")
            props = schema.get("properties", {})
            if schema.get("additionalProperties") is False:
                require(set(value) <= set(props), f"{label}: unknown fields")
            if {"excerpt", "start_rune", "end_rune", "chunk_id"} <= set(value):
                require(value["end_rune"] - value["start_rune"] == len(value["excerpt"]), f"{label}: source rune offsets differ from excerpt")
            for key, child in value.items():
                if key in props:
                    self.value(child, props[key], label + "." + key)
        if kind == "array":
            require(len(value) <= schema.get("maxItems", math.inf), f"{label}: too many items")
            for i, child in enumerate(value):
                self.value(child, schema["items"], f"{label}[{i}]")

    def walk(self, node, label="root"):
        if isinstance(node, list):
            for i, child in enumerate(node):
                self.walk(child, f"{label}[{i}]")
        elif isinstance(node, dict):
            if "$ref" in node:
                self.resolve(node["$ref"])
                self.refs += 1
            # Schema examples are an array; Media Type examples are named
            # Example Objects. These references currently use schema examples
            # and Media Type example, both checked here.
            if "schema" in node and "example" in node:
                if label.endswith("text/event-stream"):
                    require(isinstance(node["example"], str), f"{label}: SSE example is not text")
                else:
                    self.value(node["example"], node["schema"], label + ".example")
                self.examples += 1
            if "type" in node or "$ref" in node or "anyOf" in node:
                for value in node.get("examples", []):
                    self.value(value, node, label + ".examples")
                    self.examples += 1
                if "default" in node:
                    self.value(node["default"], node, label + ".default")
            if node.get("type") == "object":
                require(set(node.get("required", [])) <= set(node.get("properties", {})), f"{label}: unknown required property")
            for key, child in node.items():
                if key not in ("example", "examples", "default"):
                    self.walk(child, label + "." + key)


def check_dto_fields(document):
    mappings = {
        "internal/knowledge/model.go": {"Document": "Document", "DocumentText": "DocumentText"},
        "internal/jobs/models.go": {"Job": "Job"},
        "internal/ai/types.go": {"Conversation": "Conversation", "Message": "Message", "Action": "Action", "TicketArguments": "TicketArguments", "History": "History"},
        "internal/rag/models.go": {"Source": "Source", "Usage": "Usage", "Timings": "Timings"},
        "internal/rag/service.go": {"IndexInfo": "IndexInfo", "SearchResponse": "SearchResult", "AskResponse": "AskResult"},
    }
    count = 0
    for filename, pairs in mappings.items():
        code = (ROOT / filename).read_text()
        for go_name, schema_name in pairs.items():
            match = re.search(r"type " + go_name + r" struct\s*\{([^}]+)\}", code)
            require(match, f"DTO not found: {filename}:{go_name}")
            fields = {field for field in re.findall(r'json:"([^",]+)(?:,[^"]*)?"', match[1]) if field != "-"}
            documented = set(document["components"]["schemas"][schema_name]["properties"])
            require(fields == documented, f"DTO fields differ: {go_name}, source-only={fields-documented}, doc-only={documented-fields}")
            count += 1
    return count


def check_operation_semantics(document, markdown):
    """Check all 39 linked examples, including state transitions, not just types."""
    import hashlib

    failures, count, operations = [], 0, 0

    def test(condition, label):
        nonlocal count
        count += 1
        if not condition:
            failures.append(label)

    def json_block(section, heading):
        match = re.search(heading + r"[\s\S]*?```json\s*\n([\s\S]*?)\n```", section)
        return json.loads(match[1]) if match else None

    def inspect(value, label):
        if isinstance(value, list):
            for index, item in enumerate(value):
                inspect(item, f"{label}[{index}]")
        if not isinstance(value, dict):
            return
        if {"original_filename", "media_type", "size_bytes", "sha256_hex", "extracted_chars"} <= value.keys():
            extension = Path(value["original_filename"]).suffix.lower()
            test(value["size_bytes"] > 0, label + ": document must be nonempty")
            test(value["media_type"] == {".txt": "text/plain", ".md": "text/markdown", ".pdf": "application/pdf"}.get(extension), label + ": filename/media mismatch")
            if value["status"] == "READY":
                test(value["extracted_chars"] > 0 and value["failure_code"] is None, label + ": READY requires extracted text and no failure")
        if {"tool_name", "arguments", "confirmed_by_user_id", "ticket_id", "confirmed_at"} <= value.keys():
            decided = [value[k] is not None for k in ["confirmed_by_user_id", "ticket_id", "confirmed_at"]]
            test(all(decided) if value["status"] == "EXECUTED" else not any(decided), label + ": action execution fields inconsistent")
        if {"run_version", "attempts", "total_attempts", "completed_at", "max_attempts"} <= value.keys():
            test(value["total_attempts"] >= value["attempts"] and value["max_attempts"] == 3, label + ": job attempts inconsistent")
            terminal = value["status"] in ["FAILED", "CANCELED", "SUCCEEDED"]
            test((value["completed_at"] is not None) == terminal, label + ": job completion/status inconsistent")
            if value["status"] == "SUCCEEDED":
                test(value["phase"] == "DONE" and value["failure_code"] is None and not value["retryable"], label + ": successful job inconsistent")
        if {"role", "client_request_id", "model_name", "completed_at"} <= value.keys():
            test((value["completed_at"] is None) == (value["status"] == "STREAMING"), label + ": message completion/status inconsistent")
            if value["role"] == "USER":
                test(value["status"] == "COMPLETED" and bool(value["content"].strip()), label + ": USER message must be completed and nonempty")
        if {"embedding_dimension", "chunk_count", "index_version", "indexed_at"} <= value.keys():
            if value["status"] == "ABSENT":
                test(value["index_version"] == value["chunk_count"] == 0 and value["source_content_sha256"] == "", label + ": ABSENT index inconsistent")
            if value["status"] == "READY":
                test(value["index_version"] > 0 and value["chunk_count"] > 0 and value["indexed_at"] is not None and value["failure_code"] is None, label + ": READY index inconsistent")
        if {"known", "model_called", "prompt_tokens", "completion_tokens", "total_tokens"} <= value.keys():
            tokens = [value[k] for k in ["prompt_tokens", "completion_tokens", "total_tokens", "cache_hit_tokens", "cache_miss_tokens"]]
            if value["known"]:
                test(all(v is not None for v in tokens[:3]) and tokens[0] + tokens[1] == tokens[2], label + ": known usage must sum")
            else:
                test(all(v is None for v in tokens), label + ": unknown usage counters must be null")
        for key, item in value.items():
            inspect(item, label + "." + key)

    # Schema-level examples may be generic but may never describe impossible DTO states.
    for name, schema in document["components"]["schemas"].items():
        for example in schema.get("examples", []):
            inspect(example, "schema:" + name)

    for path, path_item in document["paths"].items():
        for method, operation in path_item.items():
            if method not in METHODS or not operation.get("x-business-operation"):
                continue
            operations += 1
            oid = operation["operationId"]
            prefix = oid + ": "
            section_match = re.search(r'<a id="operation-' + re.escape(oid) + r'"></a>([\s\S]*?)(?=<a id="operation-|## Schema字段字典)', markdown)
            test(section_match is not None, prefix + "Markdown operation section missing")
            section = section_match[1] if section_match else ""
            ctx = operation.get("x-example-context", {})
            test(bool(ctx), prefix + "operation-specific example context missing")
            test(json_block(section, r"示例前提") == ctx, prefix + "Markdown/OpenAPI context differs")
            actor = ctx.get("actor_user_id", 101)
            body_media = operation.get("requestBody", {}).get("content", {})
            request = body_media.get("application/json", {}).get("example")
            if request is not None:
                test(json_block(section, r"请求：") == request, prefix + "Markdown/OpenAPI request example differs")
            response = next(v for status, v in operation["responses"].items() if status.startswith("2"))
            output = response.get("content", {}).get("application/json", {}).get("example")
            data = output.get("data") if output else None
            if output is not None:
                test(json_block(section, r"成功：") == output, prefix + "Markdown/OpenAPI response example differs")
                inspect(data, oid)
            expected_path = ctx.get("path_parameters", {})
            test(set(expected_path) == set(re.findall(r"\{(\w+)\}", path)), prefix + "example path parameters incomplete")
            for key in ["org_id", "document_id", "ticket_id", "conversation_id"]:
                pname = {"org_id": "orgID", "document_id": "documentID", "ticket_id": "ticketID", "conversation_id": "conversationID"}[key]
                if isinstance(data, dict) and key in data and pname in expected_path:
                    test(str(data[key]) == str(expected_path[pname]), prefix + key + " differs from example path")
            if isinstance(data, dict) and "id" in data and oid not in ["createComment", "submitDocumentJob"]:
                target = next((expected_path[name] for name in ["actionID", "conversationID", "jobID", "documentID", "ticketID"] if name in expected_path), None)
                if target is not None:
                    test(str(data["id"]) == str(target), prefix + "response target differs from example path")
            children = data if isinstance(data, list) else (data.get("items", data.get("messages", [])) if isinstance(data, dict) else [])
            for child in children:
                if not isinstance(child, dict):
                    continue
                for field, pname in [("org_id", "orgID"), ("ticket_id", "ticketID"), ("document_id", "documentID"), ("conversation_id", "conversationID")]:
                    if field in child and pname in expected_path:
                        test(str(child[field]) == str(expected_path[pname]), prefix + "child resource differs from scoped path: " + field)
            before = ctx.get("before", {})
            if oid == "register":
                test(data["email"] == request["email"].strip().lower() and data["display_name"] == request["display_name"].strip(), prefix + "registered user does not match input")
            elif oid == "login":
                test(data["token_type"] == "Bearer" and data["expires_in"] == 900, prefix + "login token contract")
            elif oid == "getMe":
                test(data["id"] == actor, prefix + "current user differs from actor")
            elif oid == "createOrganization":
                test(data["name"] == request["name"].strip() and data["my_role"] == "admin", prefix + "creator must be admin and name must match")
            elif oid == "addMember":
                test(data["user_id"] == request["user_id"] and data["role"] == request["role"] and data["status"] == "active", prefix + "added membership does not match request")
            elif oid == "createTicket":
                test(data["status"] == "OPEN" and data["version"] == 0 and data["assignee_id"] is None, prefix + "new ticket initial state")
                test(all(data[k] == request[k].strip() for k in ["title", "body"]) and data["priority"] == request.get("priority", 2), prefix + "created ticket does not match request")
                test(data["creator_id"] == actor, prefix + "creator must be actor")
            elif oid in ["patchTicket", "transitionTicket", "claimTicket"]:
                test(bool(before), prefix + "before-ticket example missing")
                test(data["version"] == request["expected_version"] + 1, prefix + "successful mutation must increment version")
                if before:
                    test(before["version"] == request["expected_version"], prefix + "expected_version differs from before-ticket")
                    test(data["id"] == before["id"] and data["creator_id"] == before["creator_id"], prefix + "mutation identity changed")
                if oid == "patchTicket":
                    changed = {k: v for k, v in request.items() if k != "expected_version"}
                    test(bool(changed), prefix + "PATCH cannot contain only expected_version")
                    test(any(before.get(k) != v for k, v in changed.items()), prefix + "PATCH must actually change a field")
                    test(all(data[k] == (v.strip() if isinstance(v, str) else v) for k, v in changed.items()), prefix + "PATCH output does not match requested changes")
                    test(data["status"] == "OPEN" and before.get("status") == "OPEN", prefix + "PATCH requires OPEN")
                elif oid == "transitionTicket":
                    allowed = {("OPEN", "IN_PROGRESS"), ("IN_PROGRESS", "RESOLVED"), ("RESOLVED", "IN_PROGRESS"), ("RESOLVED", "CLOSED")}
                    test((before.get("status"), request["to_status"]) in allowed, prefix + "impossible transition")
                    test(data["status"] == request["to_status"], prefix + "transition output differs from to_status")
                    test(data["assignee_id"] is not None, prefix + "transition example must have active assignee")
                    test(data["assignee_id"] in ctx.get("active_assignee_ids", []), prefix + "transition assignee must be active in context")
                else:
                    test(before.get("status") == "OPEN" and before.get("assignee_id") is None, prefix + "claim precondition")
                    test(data["status"] == "OPEN" and data["assignee_id"] == actor, prefix + "claim must assign actor and retain OPEN")
            elif oid == "createComment":
                test(data["content"] == request["content"].strip() and data["author_id"] == actor, prefix + "comment does not match request/actor")
                test(before.get("status") in ["OPEN", "IN_PROGRESS", "RESOLVED"], prefix + "comment requires nonCLOSED parent")
            elif oid in ["listOrganizations", "listMembers", "listTickets", "listComments", "listTicketEvents", "listDocuments"]:
                test(data["page"] == ctx.get("query", {}).get("page", 1) and data["page_size"] == ctx.get("query", {}).get("page_size", 20), prefix + "pagination example does not match defaults/query")
                test(isinstance(data["items"], list), prefix + "items must be array")
            elif oid == "uploadDocument":
                test(data["status"] == "UPLOADED" and data["extracted_chars"] == 0 and data["failure_code"] is None, prefix + "uploaded document initial state")
                multipart = body_media.get("multipart/form-data", {}).get("example")
                test(multipart == {"file": ctx.get("upload_content")}, prefix + "multipart file example differs from uploaded content")
                test(json_block(section, r"请求：") == multipart, prefix + "Markdown/OpenAPI multipart example differs")
                content = ctx.get("upload_content", "").encode()
                test(bool(content) and data["size_bytes"] == len(content), prefix + "nonempty uploaded byte count mismatch")
                test(data["sha256_hex"] == hashlib.sha256(content).hexdigest(), prefix + "uploaded source hash mismatch")
                test(data["original_filename"] == ctx.get("upload_filename"), prefix + "uploaded filename mismatch")
            elif oid == "processDocument":
                test(before.get("status") in ["UPLOADED", "FAILED"], prefix + "process precondition")
                test(data["status"] == "READY" and data["extracted_chars"] > 0 and data["failure_code"] is None, prefix + "process must return READY with nonempty extracted text")
            elif oid == "getDocumentText":
                test(bool(data["content_text"].strip()) and data["content_sha256"] == hashlib.sha256(data["content_text"].encode()).hexdigest(), prefix + "text/hash mismatch")
                test(data["extractor_version"] == "utf8-v1", prefix + "extractor version mismatch")
            elif oid == "indexDocument":
                test(data["status"] == "READY" and data["chunk_count"] > 0 and data["index_version"] > 0, prefix + "index must return usable READY index")
                test(before.get("status") == "READY", prefix + "index requires READY document")
                test(data["source_content_sha256"] == ctx.get("content_sha256"), prefix + "index source hash mismatch")
            elif oid == "searchKnowledge":
                test(data["retrieval_mode"] == request.get("retrieval_mode", "vector") and len(data["items"]) <= request.get("top_k", 5), prefix + "search mode/top_k mismatch")
            elif oid == "askKnowledge":
                test(data["retrieval_mode"] == request.get("retrieval_mode", "vector"), prefix + "ask retrieval mode mismatch")
                if data["insufficient_evidence"]:
                    test(data["answer"] == "知识库未检索到足够依据。" and data["sources"] == [], prefix + "insufficient evidence answer mismatch")
                else:
                    test(bool(data["answer"].strip()) and bool(data["sources"]), prefix + "answer needs citations")
            elif oid == "submitDocumentJob":
                test(data["requested_by"] == actor and data["run_version"] >= 1, prefix + "submitted job actor/version")
            elif oid == "retryJob":
                test(before.get("status") in ["FAILED", "CANCELED"], prefix + "retry precondition")
                test(data["status"] == "PENDING" and data["phase"] == "WAITING" and data["run_version"] > 1 and data["attempts"] == 0, prefix + "retry must start next PENDING run")
                if before:
                    test(data["run_version"] == before["run_version"] + 1 and data["total_attempts"] == before["total_attempts"] and data["requested_by"] == before["requested_by"], prefix + "retry must preserve total/requested_by and increment run")
                test(data["actor_user_id"] == actor, prefix + "retry actor mismatch")
            elif oid == "cancelJob":
                test(before.get("status") in ["PENDING", "RUNNING", "RETRYING"], prefix + "cancel precondition")
                test(data["status"] == "CANCELED" and data["completed_at"] is not None and data["failure_code"] == "PROCESS_CANCELED", prefix + "cancel must return terminal CANCELED job")
            elif oid == "createConversation":
                test(data["owner_user_id"] == actor and data["title"] == request["title"].strip() and data["status"] == "ACTIVE", prefix + "created conversation mismatch")
            elif oid == "listConversations":
                test(len(data) <= 50 and all(v["owner_user_id"] == actor for v in data), prefix + "conversation owner/bounded latest mismatch")
            elif oid == "getConversationHistory":
                test(len(data["messages"]) <= 100 and len(data["actions"]) <= 100, prefix + "history bounds mismatch")
            elif oid == "listDocumentJobs":
                test(len(data["items"]) <= 20, prefix + "job latest bound mismatch")
            elif oid == "sendConversationMessage":
                frame = response["content"]["text/event-stream"]["example"]
                start = re.search(r"event: message_start\ndata: ([^\n]+)", frame)
                test(start is not None, prefix + "SSE message_start example missing")
                if start:
                    value = json.loads(start[1])
                    test(value["client_request_id"] == request["client_request_id"], prefix + "SSE client_request_id differs from request")
                    test(value["message_id"] != value["user_message_id"], prefix + "USER and ASSISTANT IDs must differ")
                    test(value["conversation_id"] == expected_path.get("conversationID"), prefix + "SSE conversation differs from path")
            elif oid in ["confirmToolAction", "rejectToolAction"]:
                test(before.get("status") == "PROPOSED", prefix + "decision example requires saved PROPOSED action")
                test(data["arguments"] == before.get("arguments"), prefix + "decision cannot change saved arguments")
                test(data["proposed_by_user_id"] == actor, prefix + "decision owner must be actor")
                if oid == "confirmToolAction":
                    test(data["status"] == "EXECUTED" and data["ticket_id"] is not None and data["confirmed_by_user_id"] == actor and data["confirmed_at"] is not None, prefix + "confirmation must return executed ticket and confirmer")
                else:
                    test(data["status"] == "REJECTED", prefix + "rejection must return REJECTED")
                    test(all(data[k] is None for k in ["ticket_id", "confirmed_by_user_id", "confirmed_at"]), prefix + "actual reject contract has no confirmation fields")
            elif oid in ["deleteTicket", "deleteDocument", "downloadDocument", "getTicket", "getDocument", "getDocumentIndex", "getJob"]:
                test(bool(ctx.get("before") or ctx.get("visible_resource")), prefix + "read/delete/download example requires visible scoped resource")
                if oid == "downloadDocument":
                    content = ctx.get("download_content", "").encode()
                    test(bool(content) and len(content) == before.get("size_bytes") and hashlib.sha256(content).hexdigest() == before.get("sha256_hex"), prefix + "download byte/hash mismatch")
            else:
                test(False, prefix + "semantic rule missing")
    require(operations == 39, f"semantic coverage must include all39, got {operations}")
    if failures:
        for failure in failures:
            print("FAIL semantic: " + failure, file=sys.stderr)
        raise CheckFailure(f"{len(failures)}/{count} operation/example semantic invariants failed across {operations} business operations")
    print(f"PASS: {count} semantic invariants across all {operations} business operations; Markdown/OpenAPI examples agree")
    return count


def main():
    document = json.loads((ROOT / "docs/api/openapi.json").read_text())
    require(document.get("openapi") == "3.1.0", "expected OpenAPI 3.1.0")
    require(document.get("jsonSchemaDialect") == "https://json-schema.org/draft/2020-12/schema", "unexpected JSON Schema dialect")
    require(document["components"]["securitySchemes"]["BearerAuth"]["scheme"] == "bearer", "missing Bearer scheme")
    source, index = registered_routes(), indexed_routes()
    require(len(source) == 39, f"expected 39 Handler business routes, found {len(source)}")
    require(source == index, f"Handler/index coverage differs: source-only={source-index}, index-only={index-source}")
    business, ids, all_operations = set(), set(), []
    markdown = (ROOT / "docs/api/NexusDesk_API完整文档.md").read_text()
    for path, item in document["paths"].items():
        for method, operation in item.items():
            if method not in METHODS:
                continue
            all_operations.append(operation)
            operation_id = operation.get("operationId")
            require(operation_id and operation_id not in ids, f"duplicate/missing operationId: {operation_id}")
            ids.add(operation_id)
            require(operation.get("tags"), f"missing tags: {operation_id}")
            if path.startswith("/api/v1/"):
                require(operation.get("x-business-operation") is True, f"unmarked business operation: {operation_id}")
                key = (method, canonical(path))
                require(key not in business, f"duplicate business method/path: {key}")
                business.add(key)
                require(f"`{method.upper()} {path}`" in markdown, f"Markdown operation missing: {key}")
            anonymous = path in ("/api/v1/auth/register", "/api/v1/auth/login", "/healthz")
            require(operation.get("security") == ([] if anonymous else [{"BearerAuth": []}]), f"wrong security: {operation_id}")
            params = operation.get("parameters", [])
            parameter_keys = [(p["name"], p["in"]) for p in params]
            require(len(parameter_keys) == len(set(parameter_keys)), f"duplicate parameters: {operation_id}")
            placeholders = set(re.findall(r"\{(\w+)\}", path))
            actual = {p["name"] for p in params if p["in"] == "path" and p.get("required") is True}
            require(actual == placeholders, f"path parameters mismatch: {operation_id}")
            for p in params:
                require(p["in"] in ("path", "query"), f"unexpected parameter location: {operation_id}")
                require("schema" in p, f"parameter lacks schema: {operation_id}")
            body = operation.get("requestBody")
            if body:
                require(method in ("post", "put", "patch"), f"body on read/delete: {operation_id}")
                expected_media = "multipart/form-data" if operation_id == "uploadDocument" else "application/json"
                require(set(body["content"]) == {expected_media}, f"wrong request media: {operation_id}")
                require(body.get("required") is True, f"optional body: {operation_id}")
                for media in body["content"].values():
                    require("$ref" in media["schema"], f"body schema must be reusable: {operation_id}")
            responses = operation["responses"]
            successes = {s for s in responses if s.startswith("2")}
            require(len(successes) == 1, f"wrong success status count: {operation_id}")
            for status, response in responses.items():
                require(re.fullmatch(r"[1-5]\d\d", status), f"invalid response status: {operation_id}/{status}")
                if status == "204":
                    require("content" not in response, f"204 must have no body: {operation_id}")
                else:
                    require("content" in response, f"response lacks content: {operation_id}/{status}")
                if int(status) >= 400:
                    require(response.get("x-error-codes"), f"missing error code list: {operation_id}/{status}")
                    require(set(response["content"]) == {"application/json"}, f"nonJSON preflight error: {operation_id}")
            if operation_id == "sendConversationMessage":
                require(set(responses["200"]["content"]) == {"text/event-stream"}, "AI success must be SSE")
                require(set(operation["x-sse-events"]) == {"message_start", "content_delta", "citation", "tool_approval_required", "message_end", "error"}, "SSE event set mismatch")
                for name in operation["x-sse-events"]:
                    require(name in markdown, f"SSE event absent in Markdown: {name}")
            elif operation_id == "downloadDocument":
                require(set(responses["200"]["content"]) == {"application/octet-stream"}, "download must be binary")
            elif "204" not in successes:
                require(all(set(responses[s]["content"]) == {"application/json"} for s in successes), f"JSON success media mismatch: {operation_id}")
    require(business == source, f"OpenAPI/Handler coverage differs: source-only={source-business}, doc-only={business-source}")
    require(len(business) == 39 and len(all_operations) == 40, "need 39 business operations plus healthz")
    schemas = document["components"]["schemas"]
    for name in schemas:
        require(f"### Schema `{name}`" in markdown, f"Markdown schema missing: {name}")
    require(schemas["ID"]["minimum"] == 1 and schemas["ID"]["maximum"] == INT64_MAX, "ID bounds mismatch")
    require(schemas["Version"]["minimum"] == 0 and schemas["Version"]["maximum"] == INT64_MAX, "version bounds mismatch")
    check_operation_semantics(document, markdown)
    validator = Validator(document)
    validator.walk(document)
    dtos = check_dto_fields(document)
    # Prove the example checker rejects important invalid forms, without any
    # backend calls or external validator dependency.
    for schema, invalid in [("ID", 0), ("ID", INT64_MAX + 1), ("Version", "0"), ("MemberInput", {"user_id": 1, "role": "support"}), ("QueryInput", {"query": "x", "top_k": None}), ("ClaimInput", {"expected_version": 0, "extra": True})]:
        try:
            validator.value(invalid, {"$ref": "#/components/schemas/" + schema}, "negative-example")
        except CheckFailure:
            pass
        else:
            raise CheckFailure(f"invalid example unexpectedly accepted: {schema}")
    print(f"PASS: {len(source)} canonical Handler routes = {len(index)} index operations = {len(business)} OpenAPI business operations")
    print(f"PASS: {len(all_operations)} unique operationIds (including healthz), {len(schemas)} schemas, {validator.refs} resolved local refs")
    print(f"PASS: {validator.examples} examples, {dtos} source DTO field sets, path parameters, security, media, statuses and Markdown coverage")
    print("Scope: static standard-library checks only; no backend mutation, DB, cloud model or runtime HTTP test")


if __name__ == "__main__":
    try:
        main()
    except (CheckFailure, KeyError, ValueError, TypeError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
