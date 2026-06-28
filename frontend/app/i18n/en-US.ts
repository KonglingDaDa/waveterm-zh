// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { I18nCatalog } from "./types";

// English strings are authored directly at call sites and used as the stable
// fallback keys for other locale catalogs.
//
// en-US is intentionally empty: t() falls back to the key string itself.
// zh-CN is the translation coverage catalog. Key-coverage tests validate that
// keys used in code resolve in zh-CN, not that en-US and zh-CN share the same key set.
export const enUS: I18nCatalog = {};