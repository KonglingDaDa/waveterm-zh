// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";
import { useWaveEnv } from "@/app/waveenv/waveenv";

const UpgradeOnboardingModal_v0_14_0_Content = () => {
    const tt = useT();
    const waveEnv = useWaveEnv();
    return (
        <div className="flex flex-col items-start w-full mb-2 unselectable">
            <div className="text-secondary leading-relaxed mb-4">
                <p className="mb-0">
                    {tt(
                        "Wave v0.14 introduces Durable Sessions. Enable them to keep your remote sessions alive through network interruptions, computer sleep, and restarts — they'll automatically reconnect when your connection is restored."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4 mb-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-sky-500 fa-sharp fa-solid fa-shield"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">
                        {tt("Durable SSH Sessions")}{" "}
                        <button
                            onClick={() => waveEnv.electron.openExternal("https://docs.waveterm.dev/durable-sessions")}
                            className="text-accent text-sm font-normal cursor-pointer hover:underline"
                        >
                            {tt("[see docs]")}
                        </button>
                    </div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Session Protection")}</strong> -{" "}
                                {tt("Programs and shell state survive disconnects")}
                            </li>
                            <li>
                                <strong>{tt("Visual Status Indicators")}</strong> - {tt("Shield icons show status")}
                            </li>
                            <li>
                                <strong>{tt("Flexible Configuration")}</strong> -{" "}
                                {tt("Enable globally, per-connection, or per-terminal")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4 mb-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-network-wired"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">
                        {tt("Enhanced Connection Monitoring")}
                    </div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Connection Keepalives")}</strong> -{" "}
                                {tt("Active monitoring with keepalive probes")}
                            </li>
                            <li>
                                <strong>{tt("Stalled Connection Detection")}</strong> -{" "}
                                {tt("Visual feedback for network issues")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4 mb-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sparkles"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Wave AI Updates")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Image Support")}</strong> - {tt("Vision capabilities for BYOK providers")}
                            </li>
                            <li>
                                <strong>{tt("Stop Generation")}</strong> -{" "}
                                {tt("Ability to stop AI responses mid-generation")}
                            </li>
                            <li>
                                <strong>{tt("Improved Auto-scrolling")}</strong>
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-terminal"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Terminal Improvements")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Enhanced Context Menu")}</strong> -{" "}
                                {tt("Quick access to splits, themes, and more")}
                            </li>
                            <li>
                                <strong>{tt("OSC 52 Clipboard Support")}</strong> -{" "}
                                {tt("CLI apps can copy to system clipboard")}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_14_0_Content.displayName = "UpgradeOnboardingModal_v0_14_0_Content";

export { UpgradeOnboardingModal_v0_14_0_Content };