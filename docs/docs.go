// Package docs Code generated by swaggo/swag. DO NOT EDIT
package docs

import "github.com/swaggo/swag"

const docTemplate = `{
    "schemes": {{ marshal .Schemes }},
    "swagger": "2.0",
    "info": {
        "description": "{{escape .Description}}",
        "title": "{{.Title}}",
        "contact": {},
        "version": "{{.Version}}"
    },
    "host": "{{.Host}}",
    "basePath": "{{.BasePath}}",
    "paths": {
        "/api/v1/coldstarter/finish": {
            "post": {
                "consumes": [
                    "*/*"
                ],
                "tags": [
                    "coldstarter",
                    "druid",
                    "daemon"
                ],
                "summary": "Finish Coldstarter",
                "operationId": "finishColdStarter",
                "responses": {
                    "202": {
                        "description": "Accepted"
                    }
                }
            }
        },
        "/api/v1/command": {
            "post": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "scroll",
                    "druid",
                    "daemon"
                ],
                "summary": "Get current scroll",
                "operationId": "runCommand",
                "parameters": [
                    {
                        "description": "Scroll Body",
                        "name": "body",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/handler.StartScrollRequestBody"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK"
                    },
                    "201": {
                        "description": "Created"
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "500": {
                        "description": "Internal Server Error"
                    }
                }
            }
        },
        "/api/v1/consoles": {
            "get": {
                "description": "Get List of all consoles",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "druid",
                    "daemon",
                    "console"
                ],
                "summary": "Get All Consoles",
                "operationId": "getConsoles",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/ConsolesResponse"
                        }
                    }
                }
            }
        },
        "/api/v1/health": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "health",
                    "druid",
                    "daemon"
                ],
                "summary": "Get ports from scroll with additional information",
                "operationId": "getHealth",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "string"
                        }
                    },
                    "503": {
                        "description": "Service Unavailable",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/api/v1/logs": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "logs",
                    "druid",
                    "daemon"
                ],
                "summary": "List all logs",
                "operationId": "listLogs",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "array",
                            "items": {
                                "$ref": "#/definitions/ScrollLogStream"
                            }
                        }
                    }
                }
            }
        },
        "/api/v1/logs/{stream}": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "logs",
                    "druid",
                    "daemon"
                ],
                "summary": "List stream logs",
                "operationId": "listLog",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Stream name",
                        "name": "stream",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/ScrollLogStream"
                        }
                    }
                }
            }
        },
        "/api/v1/metrics": {
            "get": {
                "description": "Get the metrics for all processes.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "metrics",
                    "druid",
                    "daemon"
                ],
                "summary": "Get all process metrics",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/ProcessMonitorMetricsMap"
                        }
                    }
                }
            }
        },
        "/api/v1/ports": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "port",
                    "druid",
                    "daemon"
                ],
                "summary": "Get ports from scroll with additional information",
                "operationId": "getPorts",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/domain.AugmentedPort"
                        }
                    }
                }
            }
        },
        "/api/v1/procedure": {
            "post": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "scroll",
                    "druid",
                    "daemon"
                ],
                "summary": "Run procedure",
                "operationId": "runProcedure",
                "parameters": [
                    {
                        "description": "Procedure Body",
                        "name": "body",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/handler.StartProcedureRequestBody"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {}
                    },
                    "201": {
                        "description": "Created"
                    }
                }
            }
        },
        "/api/v1/processes": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "process",
                    "druid",
                    "daemon"
                ],
                "summary": "Get running processes",
                "operationId": "getRunningProcesses",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/handler.ProcessesBody"
                        }
                    }
                }
            }
        },
        "/api/v1/pstree": {
            "get": {
                "description": "Get pstree of running process",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "metrics",
                    "druid",
                    "daemon"
                ],
                "summary": "Get all process metrics",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/PsTreeMap"
                        }
                    }
                }
            }
        },
        "/api/v1/queue": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "queue",
                    "druid",
                    "daemon"
                ],
                "summary": "Get current scroll",
                "operationId": "getQueue",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "$ref": "#/definitions/domain.ScrollLockStatus"
                            }
                        }
                    }
                }
            }
        },
        "/api/v1/scroll": {
            "get": {
                "consumes": [
                    "*/*"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "scroll",
                    "druid",
                    "daemon"
                ],
                "summary": "Get current scroll",
                "operationId": "getScroll",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/ScrollFile"
                        }
                    }
                }
            }
        },
        "/api/v1/token": {
            "get": {
                "description": "Get the metrics for all processes.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "websocket",
                    "druid",
                    "daemon"
                ],
                "summary": "Get current scroll",
                "operationId": "createToken",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/WebsocketToken"
                        }
                    }
                }
            }
        }
    },
    "definitions": {
        "CommandInstructionSet": {
            "type": "object",
            "properties": {
                "needs": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "procedures": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/Procedure"
                    }
                },
                "run": {
                    "$ref": "#/definitions/domain.RunMode"
                }
            }
        },
        "Console": {
            "type": "object",
            "required": [
                "inputMode",
                "type"
            ],
            "properties": {
                "exit": {
                    "type": "integer"
                },
                "inputMode": {
                    "type": "string"
                },
                "type": {
                    "$ref": "#/definitions/domain.ConsoleType"
                }
            }
        },
        "ConsolesResponse": {
            "type": "object",
            "required": [
                "consoles"
            ],
            "properties": {
                "consoles": {
                    "type": "object",
                    "additionalProperties": {
                        "$ref": "#/definitions/Console"
                    }
                }
            }
        },
        "Procedure": {
            "type": "object",
            "properties": {
                "data": {},
                "id": {
                    "type": "string"
                },
                "mode": {
                    "type": "string"
                },
                "wait": {}
            }
        },
        "ProcessMonitorMetrics": {
            "type": "object",
            "properties": {
                "connections": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "cpu": {
                    "type": "number"
                },
                "memory": {
                    "type": "integer"
                },
                "pid": {
                    "type": "integer"
                }
            }
        },
        "ProcessMonitorMetricsMap": {
            "type": "object",
            "additionalProperties": {
                "$ref": "#/definitions/ProcessMonitorMetrics"
            }
        },
        "ProcessTreeRoot": {
            "type": "object",
            "properties": {
                "root": {
                    "$ref": "#/definitions/ProcessTreeNode"
                },
                "total_cpu_percent": {
                    "type": "number"
                },
                "total_io_counters_read": {
                    "type": "integer"
                },
                "total_io_counters_write": {
                    "type": "integer"
                },
                "total_memory_rss": {
                    "type": "integer"
                },
                "total_memory_swap": {
                    "type": "integer"
                },
                "total_memory_vms": {
                    "type": "integer"
                },
                "total_process_count": {
                    "type": "integer"
                }
            }
        },
        "PsTreeMap": {
            "type": "object",
            "additionalProperties": {
                "$ref": "#/definitions/ProcessTreeRoot"
            }
        },
        "ScrollFile": {
            "type": "object",
            "properties": {
                "app_version": {
                    "description": "don't make this a semver, it's not allways",
                    "type": "string"
                },
                "commands": {
                    "type": "object",
                    "additionalProperties": {
                        "$ref": "#/definitions/CommandInstructionSet"
                    }
                },
                "cronjobs": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/domain.Cronjob"
                    }
                },
                "desc": {
                    "type": "string"
                },
                "init": {
                    "type": "string"
                },
                "name": {
                    "type": "string"
                },
                "plugins": {
                    "type": "object",
                    "additionalProperties": {
                        "type": "object",
                        "additionalProperties": {
                            "type": "string"
                        }
                    }
                },
                "ports": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/domain.Port"
                    }
                },
                "version": {
                    "type": "string"
                }
            }
        },
        "ScrollLogStream": {
            "type": "object",
            "required": [
                "key",
                "log"
            ],
            "properties": {
                "key": {
                    "type": "string"
                },
                "log": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                }
            }
        },
        "WebsocketToken": {
            "type": "object",
            "required": [
                "token"
            ],
            "properties": {
                "token": {
                    "type": "string"
                }
            }
        },
        "domain.AugmentedPort": {
            "type": "object",
            "properties": {
                "inactive_since": {
                    "type": "string"
                },
                "inactive_since_sec": {
                    "type": "integer"
                },
                "mandatory": {
                    "type": "boolean"
                },
                "name": {
                    "type": "string"
                },
                "open": {
                    "type": "boolean"
                },
                "port": {
                    "type": "integer"
                },
                "protocol": {
                    "type": "string"
                },
                "sleep_handler": {
                    "type": "string"
                },
                "start_delay": {
                    "type": "integer"
                },
                "vars": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/domain.ColdStarterVars"
                    }
                }
            }
        },
        "domain.ColdStarterVars": {
            "type": "object",
            "properties": {
                "name": {
                    "type": "string"
                },
                "value": {
                    "type": "string"
                }
            }
        },
        "domain.ConsoleType": {
            "type": "string",
            "enum": [
                "tty",
                "process",
                "plugin"
            ],
            "x-enum-varnames": [
                "ConsoleTypeTTY",
                "ConsoleTypeProcess",
                "ConsoleTypePlugin"
            ]
        },
        "domain.Cronjob": {
            "type": "object",
            "properties": {
                "command": {
                    "type": "string"
                },
                "name": {
                    "type": "string"
                },
                "schedule": {
                    "type": "string"
                }
            }
        },
        "domain.Port": {
            "type": "object",
            "properties": {
                "mandatory": {
                    "type": "boolean"
                },
                "name": {
                    "type": "string"
                },
                "port": {
                    "type": "integer"
                },
                "protocol": {
                    "type": "string"
                },
                "sleep_handler": {
                    "type": "string"
                },
                "start_delay": {
                    "type": "integer"
                },
                "vars": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/domain.ColdStarterVars"
                    }
                }
            }
        },
        "domain.Process": {
            "type": "object",
            "properties": {
                "name": {
                    "type": "string"
                },
                "type": {
                    "type": "string"
                }
            }
        },
        "domain.ProcessTreeNode": {
            "type": "object",
            "properties": {
                "children": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/domain.ProcessTreeNode"
                    }
                },
                "cmdline": {
                    "type": "string"
                },
                "cpu_percent": {
                    "type": "number"
                },
                "gids": {
                    "type": "array",
                    "items": {
                        "type": "integer"
                    }
                },
                "io_counters": {
                    "type": "string"
                },
                "memory": {
                    "type": "string"
                },
                "memory_ex": {
                    "type": "string"
                },
                "name": {
                    "type": "string"
                },
                "process": {
                    "type": "string"
                },
                "username": {
                    "type": "string"
                }
            }
        },
        "domain.RunMode": {
            "type": "string",
            "enum": [
                "always",
                "once",
                "restart"
            ],
            "x-enum-comments": {
                "RunModeAlways": "default"
            },
            "x-enum-varnames": [
                "RunModeAlways",
                "RunModeOnce",
                "RunModeRestart"
            ]
        },
        "domain.ScrollLockStatus": {
            "type": "string",
            "enum": [
                "running",
                "done",
                "error",
                "waiting"
            ],
            "x-enum-varnames": [
                "ScrollLockStatusRunning",
                "ScrollLockStatusDone",
                "ScrollLockStatusError",
                "ScrollLockStatusWaiting"
            ]
        },
        "handler.ProcessesBody": {
            "type": "object",
            "properties": {
                "processes": {
                    "type": "object",
                    "additionalProperties": {
                        "$ref": "#/definitions/domain.Process"
                    }
                }
            }
        },
        "handler.StartProcedureRequestBody": {
            "type": "object",
            "properties": {
                "data": {
                    "type": "string"
                },
                "mode": {
                    "type": "string"
                },
                "process": {
                    "type": "string"
                },
                "sync": {
                    "type": "boolean"
                }
            }
        },
        "handler.StartScrollRequestBody": {
            "type": "object",
            "properties": {
                "command": {
                    "type": "string"
                },
                "sync": {
                    "type": "boolean"
                }
            }
        }
    }
}`

// SwaggerInfo holds exported Swagger Info so clients can modify it
var SwaggerInfo = &swag.Spec{
	Version:          "0.1.0",
	Host:             "",
	BasePath:         "",
	Schemes:          []string{},
	Title:            "Druid CLI",
	Description:      "Druid CLI is a process runner to launches and manages various sorts of applications, like gameservers, databases or webservers.",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
	LeftDelim:        "{{",
	RightDelim:       "}}",
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
