interface ActionlintError {
    kind: string;
    message: string;
    line: number;
    column: number;
}

interface Window {
    runActionlint?(src: string, format?: string): void;
    getYamlSource(): string;
    getInputFormat(): string;
    showError(msg: string): void;
    onCheckCompleted(errs: ActionlintError[]): void;
    dismissLoading(): void;
}

declare class Go {
    importObject: Imports;
    run(mod: Instance): Promise<unknown>;
}

declare const isMobile: IsMobile.isMobileResult;
