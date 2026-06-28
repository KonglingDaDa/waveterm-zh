// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";
import type { WaveConfigViewModel } from "@/app/view/waveconfig/waveconfig-model";
import { memo } from "react";

interface WaveAIVisualContentProps {
    model: WaveConfigViewModel;
}

export const WaveAIVisualContent = memo(({ model: _model }: WaveAIVisualContentProps) => {
    const tt = useT();
    return (
        <div className="flex flex-col gap-4 p-6 h-full">
            <div className="text-lg font-semibold">{tt("Wave AI Modes - Visual Editor")}</div>
            <div className="text-muted-foreground">{tt("Visual editor coming soon...")}</div>
        </div>
    );
});

WaveAIVisualContent.displayName = "WaveAIVisualContent";