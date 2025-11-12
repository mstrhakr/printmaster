const helpers = require('../shared_helpers');

jest.useFakeTimers();

describe('debounce', () => {
    test('debounce calls function after wait', () => {
        const fn = jest.fn();
        const d = helpers.debounce(fn, 100);
        d();
        expect(fn).not.toHaveBeenCalled();
        jest.advanceTimersByTime(100);
        expect(fn).toHaveBeenCalledTimes(1);
    });

    test('debounce resets timer on repeated calls', () => {
        const fn = jest.fn();
        const d = helpers.debounce(fn, 100);
        d();
        jest.advanceTimersByTime(50);
        d();
        jest.advanceTimersByTime(50);
        expect(fn).not.toHaveBeenCalled();
        jest.advanceTimersByTime(50);
        expect(fn).toHaveBeenCalledTimes(1);
    });
});
